#include <orbit/serial_queue.hpp>

#include <algorithm>
#include <condition_variable>
#include <mutex>
#include <stdexcept>
#include <thread>
#include <utility>
#include <vector>

namespace orbit {
namespace {

using Clock = std::chrono::steady_clock;

Clock::time_point deadline_after(Clock::time_point now, Clock::duration delay) {
  return now > Clock::time_point::max() - delay ? Clock::time_point::max() : now + delay;
}

template <class Condition, class Predicate>
bool wait_until(Condition& condition, std::unique_lock<std::mutex>& lock,
                Clock::time_point deadline, Predicate ready) {
  while (!ready()) {
    const auto now = Clock::now();
    if (now >= deadline) {
      return false;
    }
    // Some standard libraries convert steady deadlines to wall-clock deadlines.
    // Keep that conversion within range even if the caller supplies a maximum duration.
    const auto slice = deadline_after(now, std::chrono::hours{24});
    condition.wait_until(lock, std::min(deadline, slice), ready);
  }
  return true;
}

}  // namespace

struct SerialQueue::Impl {
  enum class State { idle, running, draining, stopped };

  struct Execution {
    std::exception_ptr error;
    std::uint64_t nanos;
  };

  Impl(std::size_t capacity, SerialQueueOptions queue_options)
      : options(std::move(queue_options)), interval(std::chrono::ceil<Clock::duration>(options.interval)),
        storage(capacity + 1), jobs(capacity), stats{.capacity = capacity} {
    free_jobs.reserve(storage.size());
    for (auto& job : storage) {
      free_jobs.push_back(&job);
    }
  }

  ~Impl() { join(); }

  QueueStartResult start(std::stop_token token) {
    std::lock_guard lock(mutex);
    if (state != State::idle) {
      return QueueStartResult::closed;
    }
    if (token.stop_requested()) {
      return QueueStartResult::cancelled;
    }
    worker = std::thread([this] {
      // Keep the marker set through thread-exit cleanup to detect self-joins there.
      worker_queue = this;
      run();
    });
    state = State::running;
    return QueueStartResult::started;
  }

  QueuePostResult post(Callback&& callback) {
    if (!callback) {
      return QueuePostResult::invalid_callback;
    }
    Callback* job;
    {
      std::lock_guard lock(mutex);
      if (state != State::running) {
        return QueuePostResult::closed;
      }
      if (queued + preparing == jobs.size()) {
        ++stats.rejected;
        return QueuePostResult::full;
      }
      job = free_jobs.back();
      free_jobs.pop_back();
      ++preparing;
    }

    // The reserved slot consumes capacity, but joins the FIFO only after
    // moving the callable outside the lock.
    *job = std::move(callback);

    auto result = QueuePostResult::closed;
    bool wake_worker = false;
    {
      std::lock_guard lock(mutex);
      --preparing;
      if (state == State::running) {
        jobs[(head + queued) % jobs.size()] = job;
        wake_worker = queued++ == 0;
        result = QueuePostResult::accepted;
      }
    }
    if (wake_worker) {
      ready.notify_one();
    }
    if (result == QueuePostResult::closed) {
      *job = nullptr;
      std::lock_guard lock(mutex);
      free_jobs.push_back(job);
    }
    return result;
  }

  bool stop(std::stop_token token) {
    std::unique_lock lock(mutex);
    begin_stop();
    return completed.wait(lock, token, [this] { return state == State::stopped; });
  }

  bool stop_for(std::chrono::nanoseconds timeout) {
    const auto delay = std::chrono::ceil<Clock::duration>(
        std::max(timeout, std::chrono::nanoseconds::zero()));
    const auto deadline = deadline_after(Clock::now(), delay);
    std::unique_lock lock(mutex);
    begin_stop();
    return wait_until(completed, lock, deadline, [this] { return state == State::stopped; });
  }

  void join() {
    if (worker_queue == this) {
      throw std::logic_error("SerialQueue cannot join from its worker");
    }
    stop({});
    std::call_once(joined, [this] {
      if (worker.joinable()) {
        worker.join();
      }
    });
  }

  SerialQueueStatus status() const {
    std::lock_guard lock(mutex);
    auto snapshot = stats;
    snapshot.queued = queued;
    snapshot.accepting = state == State::running;
    snapshot.preparing = preparing;
    return snapshot;
  }

 private:
  inline static thread_local const Impl* worker_queue = nullptr;

  // Called with mutex held.
  void begin_stop() {
    if (state != State::stopped && worker_queue == this) {
      throw std::logic_error("SerialQueue cannot stop from its worker");
    }
    if (state == State::idle) {
      state = State::stopped;
    } else if (state == State::running) {
      state = State::draining;
      ready.notify_one();
    }
  }

  void run() {
    auto next_tick = deadline_after(Clock::now(), interval);
    bool prefer_job = false;
    std::unique_lock lock(mutex);
    const auto work_ready = [this] { return state != State::running || queued != 0; };
    while (state == State::running || queued != 0) {
      // Alternate when both sources are ready so either can make progress.
      if (state == State::running && options.update && (!prefer_job || queued == 0)) {
        const auto now = Clock::now();
        if (now >= next_tick) {
          const auto late = now - next_tick;
          const auto tick = next_tick;
          // Keep the ticker's phase and coalesce ticks missed by a slow callback.
          next_tick = deadline_after(now, interval - late % interval);
          prefer_job = true;
          lock.unlock();
          auto execution = invoke([&] { options.update(tick); });
          lock.lock();
          record(execution, lock);
          continue;
        }
      }

      if (queued != 0) {
        auto* job = jobs[head];
        head = (head + 1) % jobs.size();
        --queued;
        lock.unlock();
        auto execution = invoke(*job);
        *job = nullptr;
        lock.lock();
        free_jobs.push_back(job);
        record(execution, lock);
        prefer_job = false;
      } else if (options.update) {
        wait_until(ready, lock, next_tick, work_ready);
      } else {
        ready.wait(lock, work_ready);
      }
    }
    state = State::stopped;
    completed.notify_all();
  }

  template <class Function>
  Execution invoke(Function&& callback) {
    const auto start = Clock::now();
    std::exception_ptr error;
    try {
      callback();
    } catch (...) {
      error = std::current_exception();
    }
    const auto elapsed = std::chrono::duration_cast<std::chrono::nanoseconds>(Clock::now() - start);
    return {std::move(error), static_cast<std::uint64_t>(elapsed.count())};
  }

  void record(Execution& execution, std::unique_lock<std::mutex>& lock) {
    ++stats.executed;
    stats.execution_nanos += execution.nanos;
    if (execution.error) {
      ++stats.exceptions;
      lock.unlock();
      if (options.on_error) {
        try {
          options.on_error(execution.error);
        } catch (...) {
          // Diagnostics must not interrupt the queue.
        }
      }
      // The exception object can also have a user-defined destructor.
      execution.error = nullptr;
      lock.lock();
    }
  }

  const SerialQueueOptions options;
  const Clock::duration interval;
  mutable std::mutex mutex;
  std::condition_variable ready;
  std::condition_variable_any completed;
  State state = State::idle;
  std::vector<Callback> storage;
  std::vector<Callback*> free_jobs;
  std::vector<Callback*> jobs;
  std::size_t head = 0;
  std::size_t queued = 0;
  std::size_t preparing = 0;
  SerialQueueStatus stats;
  std::once_flag joined;
  std::thread worker;
};

SerialQueue::SerialQueue(std::ptrdiff_t capacity, SerialQueueOptions options) {
  if (capacity < 1 || options.interval < std::chrono::nanoseconds::zero() ||
      (options.update && options.interval == std::chrono::nanoseconds::zero())) {
    throw std::invalid_argument("SerialQueue has invalid capacity or interval");
  }
  impl_ = std::make_unique<Impl>(static_cast<std::size_t>(capacity), std::move(options));
}

SerialQueue::~SerialQueue() = default;

QueueStartResult SerialQueue::start(std::stop_token token) { return impl_->start(token); }
QueuePostResult SerialQueue::post(Callback callback) { return impl_->post(std::move(callback)); }
bool SerialQueue::stop(std::stop_token token) { return impl_->stop(token); }
bool SerialQueue::stop_for(std::chrono::nanoseconds timeout) { return impl_->stop_for(timeout); }
void SerialQueue::join() { impl_->join(); }
SerialQueueStatus SerialQueue::status() const { return impl_->status(); }

}  // namespace orbit
