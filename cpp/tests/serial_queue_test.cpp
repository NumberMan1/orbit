#include <orbit/serial_queue.hpp>

#include <atomic>
#include <future>
#include <iostream>
#include <latch>
#include <stdexcept>
#include <thread>
#include <vector>

#ifdef __MINGW32__
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#endif

namespace {

using namespace std::chrono_literals;
using orbit::QueuePostResult;
using orbit::QueueStartResult;
using orbit::SerialQueue;

void require(bool condition, const char* message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

template <class Predicate>
bool wait_for_condition(Predicate ready) {
  const auto deadline = std::chrono::steady_clock::now() + 2s;
  while (!ready()) {
    if (std::chrono::steady_clock::now() >= deadline) {
      return false;
    }
    std::this_thread::sleep_for(1ms);
  }
  return true;
}

void check_invalid_options(std::ptrdiff_t capacity, orbit::SerialQueueOptions options) {
  try {
    SerialQueue queue(capacity, options);
  } catch (const std::invalid_argument&) {
    return;
  }
  throw std::runtime_error("accepted invalid queue options");
}

void check_lifecycle() {
  check_invalid_options(0, {});
  check_invalid_options(-1, {});
  check_invalid_options(1, {.interval = -1ns});
  check_invalid_options(1, {.update = [](auto) {}});

  SerialQueue stopped(1);
  stopped.join();
  require(stopped.stop_for(0ns), "idle queue did not stop immediately");
  require(stopped.start() == QueueStartResult::closed, "started after stop");

  int executed = 0;
  {
    SerialQueue queue(2, {.interval = 1ms});
    require(queue.status().capacity == 2 && !queue.status().accepting, "invalid initial status");
    require(queue.post([] {}) == QueuePostResult::closed, "accepted before start");
    require(queue.post({}) == QueuePostResult::invalid_callback, "accepted an empty callback");
    std::stop_source cancelled;
    cancelled.request_stop();
    require(queue.start(cancelled.get_token()) == QueueStartResult::cancelled, "started with a cancelled token");
    std::stop_source startup;
    require(queue.start(startup.get_token()) == QueueStartResult::started, "start failed after cancellation");
    startup.request_stop();
    require(queue.start() == QueueStartResult::closed, "started twice");
    require(queue.post([&] { ++executed; }) == QueuePostResult::accepted, "startup token stopped worker");
  }
  require(executed == 1, "destruction did not drain work");
}

void check_fifo_backpressure_and_exceptions() {
  std::promise<void> entered, release;
  auto released = release.get_future();
  std::promise<QueuePostResult> reentrant_full, reentrant_post;
  std::vector<int> order;
  int reported = 0;
  SerialQueue queue(2, {.on_error = [&](std::exception_ptr error) {
    try {
      std::rethrow_exception(error);
    } catch (int value) {
      reported = value;
    }
    throw std::runtime_error("error hook failed");
  }});
  require(queue.start() == QueueStartResult::started, "could not start FIFO test");
  require(queue.post([&] {
    entered.set_value();
    released.wait_for(3s);
    order.push_back(1);
    reentrant_full.set_value(queue.post([] {}));
    throw 42;
  }) == QueuePostResult::accepted, "first post rejected");
  require(entered.get_future().wait_for(2s) == std::future_status::ready, "worker did not begin");
  require(queue.post([&] {
    order.push_back(2);
    reentrant_post.set_value(queue.post([&] { order.push_back(4); }));
  }) == QueuePostResult::accepted, "active job occupied a waiting slot");
  require(queue.post([&] { order.push_back(3); }) == QueuePostResult::accepted, "last slot unavailable");
  require(queue.post([] {}) == QueuePostResult::full, "full queue accepted a callback");
  const auto busy = queue.status();
  require(busy.accepting && busy.queued == 2 && busy.executed == 0 && busy.rejected == 1,
          "invalid status while worker was blocked");
  release.set_value();
  auto full = reentrant_full.get_future();
  auto posted = reentrant_post.get_future();
  require(full.wait_for(2s) == std::future_status::ready && full.get() == QueuePostResult::full,
          "reentrant post did not report full");
  require(posted.wait_for(2s) == std::future_status::ready && posted.get() == QueuePostResult::accepted,
          "reentrant post did not fill the free slot");
  require(queue.stop_for(2s), "FIFO queue did not drain");
  require(order == std::vector{1, 2, 3, 4} && reported == 42, "FIFO order or exception was lost");
  require(queue.post([] {}) == QueuePostResult::closed && queue.stop(), "stop was not permanent or idempotent");
  const auto done = queue.status();
  require(!done.accepting && done.queued == 0 && done.executed == 4 && done.exceptions == 1 &&
              done.rejected == 2 && done.execution_nanos > 0, "invalid completion totals");
}

struct ReentrantCallback {
  SerialQueue& queue;
  int& executed;

  ReentrantCallback(SerialQueue& owner, int& count) : queue(owner), executed(count) {}
  ReentrantCallback(const ReentrantCallback&) = default;
  ReentrantCallback(ReentrantCallback&& other) noexcept
      : queue(other.queue), executed(other.executed) {
    (void)queue.status();
  }
  ~ReentrantCallback() { (void)queue.status(); }

  void operator()() const { ++executed; }
};

void check_callable_lifecycle_can_reenter() {
  int executed = 0;
  SerialQueue queue(1);
  require(queue.start() == QueueStartResult::started, "could not start callable lifecycle test");
  require(queue.post(ReentrantCallback(queue, executed)) == QueuePostResult::accepted,
          "callable that reenters during movement was rejected");
  require(queue.stop_for(2s) && executed == 1, "reentrant callable did not drain");
}

#ifdef _MSC_VER
struct PreparingCallback {
  std::promise<void>& entered;
  std::future<void>& release;
  int moves = 0;

  PreparingCallback(std::promise<void>& started, std::future<void>& released)
      : entered(started), release(released) {}
  PreparingCallback(const PreparingCallback&) = default;
  PreparingCallback(PreparingCallback&& other) noexcept
      : entered(other.entered), release(other.release), moves(other.moves + 1) {
    // MSVC moves the small callable into std::function and again into the slot.
    if (moves == 2) {
      entered.set_value();
      release.wait_for(3s);
    }
  }

  void operator()() const {}
};

void check_preparing_post_reserves_capacity() {
  std::promise<void> entered, release;
  auto released = release.get_future();
  SerialQueue queue(1);
  require(queue.start() == QueueStartResult::started, "could not start staged post test");
  auto posting = std::async(std::launch::async, [&] {
    return queue.post(PreparingCallback(entered, released));
  });
  require(entered.get_future().wait_for(2s) == std::future_status::ready, "post did not reserve a slot");
  const auto staged = queue.status();
  require(staged.queued == 0 && staged.preparing == 1, "wrong pressure for a reserved slot");
  require(queue.post([] {}) == QueuePostResult::full, "concurrent post bypassed the reserved slot");
  require(queue.stop_for(1s), "stop waited for a post that had not been accepted");
  release.set_value();
  require(posting.get() == QueuePostResult::closed, "post published after stop");
  const auto done = queue.status();
  require(done.preparing == 0 && done.executed == 0 && done.rejected == 1,
          "staged post left an occupied slot or invalid counts");
}
#endif

void check_periodic_update_and_stop() {
  using Clock = std::chrono::steady_clock;
  std::promise<void> release;
  auto released = release.get_future();
  std::promise<Clock::time_point> tick;
  auto first_tick = tick.get_future();
  std::thread::id update_thread, job_thread;
  int updates = 0;
  SerialQueue queue(1, {.interval = 5ms, .update = [&](Clock::time_point now) {
    if (++updates == 1) {
      update_thread = std::this_thread::get_id();
      tick.set_value(now);
      released.wait_for(3s);
      throw std::runtime_error("periodic update failed");
    }
  }});
  const auto before_start = Clock::now();
  require(queue.start() == QueueStartResult::started, "could not start periodic test");
  require(first_tick.wait_for(2s) == std::future_status::ready, "idle ticker did not wake the worker");
  const auto scheduled = first_tick.get();
  require(scheduled >= before_start + 5ms && scheduled <= Clock::now(), "invalid scheduled tick");
  require(queue.post([&] { job_thread = std::this_thread::get_id(); }) == QueuePostResult::accepted,
          "update used a waiting slot");

  std::stop_source cancel_wait;
  auto stopping = std::async(std::launch::async, [&] { return queue.stop(cancel_wait.get_token()); });
  require(wait_for_condition([&] { return !queue.status().accepting; }), "stop did not close admission");
  const auto draining = queue.status();
  cancel_wait.request_stop();
  require(stopping.wait_for(1s) == std::future_status::ready && !stopping.get(),
          "cancellation did not interrupt the stop wait");
  require(!draining.accepting && draining.queued == 1 && draining.executed == 0,
          "stop did not close admission while preserving pending work");
  require(!queue.stop_for(10ms), "stop reported success while an update was running");
  release.set_value();
  require(queue.stop_for(2s), "queue did not drain after interrupted stop");
  const auto done = queue.status();
  require(update_thread == job_thread && updates == 1 && done.executed == 2 && done.exceptions == 1,
          "updates did not share the worker, stop boundary, or callback counters");
}

struct ThreadCleanupGate {
  std::atomic<bool> entered{false};
  std::atomic<bool> released{false};
};

struct ReleaseThreadCleanup {
  ThreadCleanupGate& gate;
  ~ReleaseThreadCleanup() { gate.released = true; }
};

void wait_for_cleanup_release(ThreadCleanupGate& gate) {
  gate.entered = true;
  while (!gate.released) {
    std::this_thread::yield();
  }
}

#ifdef __MINGW32__
std::chrono::microseconds process_cpu_time() {
  FILETIME creation, exit, kernel, user;
  require(::GetProcessTimes(::GetCurrentProcess(), &creation, &exit, &kernel, &user) != 0,
          "could not read process CPU time");
  const auto ticks = [](FILETIME time) {
    return (static_cast<std::uint64_t>(time.dwHighDateTime) << 32) + time.dwLowDateTime;
  };
  return std::chrono::microseconds((ticks(kernel) + ticks(user)) / 10);
}

void check_max_waits_do_not_spin() {
  constexpr auto longest = std::chrono::nanoseconds::max();
  std::promise<void> entered, release;
  auto released = release.get_future();
  SerialQueue queue(1, {.interval = longest, .update = [](auto) {}});
  require(queue.start() == QueueStartResult::started, "could not start maximum interval test");
  const auto before_tick = process_cpu_time();
  std::this_thread::sleep_for(250ms);
  require(process_cpu_time() - before_tick < 150ms, "maximum tick interval spun instead of sleeping");
  require(queue.status().executed == 0, "maximum tick interval fired early");

  require(queue.post([&] {
    entered.set_value();
    released.wait_for(3s);
  }) == QueuePostResult::accepted, "maximum tick interval prevented posting");
  require(entered.get_future().wait_for(2s) == std::future_status::ready, "worker did not start");
  auto stopping = std::async(std::launch::async, [&] { return queue.stop_for(longest); });
  require(wait_for_condition([&] { return !queue.status().accepting; }), "stop did not close admission");
  const auto before_stop = process_cpu_time();
  const bool waited = stopping.wait_for(250ms) == std::future_status::timeout;
  const auto stop_cpu = process_cpu_time() - before_stop;
  release.set_value();
  require(waited && stopping.get(), "maximum stop timeout expired before drain");
  require(stop_cpu < 150ms, "maximum stop timeout spun instead of sleeping");
}

class ThreadExitHook {
 public:
  // Use native FLS cleanup to avoid relying on MinGW's C++ TLS teardown.
  ThreadExitHook() : slot_(::FlsAlloc(cleanup)) {
    require(slot_ != FLS_OUT_OF_INDEXES, "could not allocate a thread exit hook");
  }
  ~ThreadExitHook() { ::FlsFree(slot_); }

  void install(ThreadCleanupGate& gate) const {
    require(::FlsSetValue(slot_, &gate) != 0, "could not set a thread exit hook");
  }

 private:
  static void CALLBACK cleanup(void* value) {
    wait_for_cleanup_release(*static_cast<ThreadCleanupGate*>(value));
  }

  DWORD slot_;
};
#else
struct ThreadExitHook {
  void install(ThreadCleanupGate& gate) const {
    struct OnExit {
      ThreadCleanupGate& gate;
      ~OnExit() { wait_for_cleanup_release(gate); }
    };
    thread_local OnExit cleanup{gate};
  }
};
#endif

void check_worker_exit_after_drain(bool cancellable) {
  ThreadCleanupGate gate;
  const ThreadExitHook exit_hook;
  std::promise<void> destructor_entered, destructor_finished;
  std::promise<bool> stop_finished;
  auto stopped = stop_finished.get_future();
  auto destroyed = destructor_finished.get_future();
  auto queue = std::make_unique<SerialQueue>(1);
  std::stop_source cancel_wait;
  std::jthread owner;
  const ReleaseThreadCleanup release{gate};
  std::promise<void> begin_destruction;
  require(queue->start() == QueueStartResult::started, "could not start thread cleanup test");
  require(queue->post([&] { exit_hook.install(gate); }) == QueuePostResult::accepted,
          "could not install thread cleanup");

  owner = std::jthread([&, destroy = begin_destruction.get_future()] {
    stop_finished.set_value(cancellable ? queue->stop(cancel_wait.get_token()) : queue->stop_for(1s));
    destroy.wait();
    destructor_entered.set_value();
    if (cancellable) {
      queue.reset();
    } else {
      queue->join();
      queue->join();
    }
    destructor_finished.set_value();
  });
  require(wait_for_condition([&] { return gate.entered.load(); }), "worker thread cleanup did not start");
  cancel_wait.request_stop();
  require(stopped.wait_for(1s) == std::future_status::ready && stopped.get(),
          "stop did not report drain independently of thread cleanup");

  begin_destruction.set_value();
  require(destructor_entered.get_future().wait_for(2s) == std::future_status::ready,
          "thread resource reclamation did not start");
  const bool joining = destroyed.wait_for(20ms) == std::future_status::timeout;
  gate.released = true;
  require(destroyed.wait_for(2s) == std::future_status::ready, "thread resources were not reclaimed after cleanup");
  require(joining, "thread reclamation did not wait for cleanup");
}

void check_due_ticks_allow_jobs() {
  std::promise<void> tick_entered, release, job_ran;
  auto released = release.get_future();
  int updates = 0;
  int updates_before_job = 0;
  SerialQueue queue(1, {.interval = 1ns, .update = [&](auto) {
    if (++updates == 1) {
      tick_entered.set_value();
      released.wait_for(3s);
    }
  }});
  require(queue.start() == QueueStartResult::started, "could not start busy ticker");
  require(tick_entered.get_future().wait_for(2s) == std::future_status::ready, "first tick did not run");
  require(queue.post([&] {
    updates_before_job = updates;
    job_ran.set_value();
  }) == QueuePostResult::accepted, "busy ticker rejected a job");
  release.set_value();
  require(job_ran.get_future().wait_for(2s) == std::future_status::ready, "due ticks starved a queued job");
  require(queue.stop_for(2s) && updates_before_job == 1, "missed ticks took precedence over a queued job");
}

void check_concurrent_post_and_stop() {
  constexpr int producer_count = 4;
  std::promise<void> entered, release;
  auto released = release.get_future();
  SerialQueue queue(8);
  require(queue.start() == QueueStartResult::started, "could not start concurrent test");
  require(queue.post([&] {
    entered.set_value();
    released.wait_for(3s);
  }) == QueuePostResult::accepted, "could not hold the worker during concurrent stop");
  require(entered.get_future().wait_for(2s) == std::future_status::ready, "worker did not start");
  std::promise<void> ready;
  const auto start = ready.get_future().share();
  std::latch first_posts(producer_count);
  std::atomic<std::uint64_t> accepted{0}, full{0}, closed{0}, stopped{0};
  std::vector<std::jthread> threads;
  for (int i = 0; i < producer_count; ++i) {
    threads.emplace_back([&] {
      start.wait();
      for (int j = 0; j < 500; ++j) {
        switch (queue.post([] {})) {
          case QueuePostResult::accepted: ++accepted; break;
          case QueuePostResult::full: ++full; break;
          case QueuePostResult::closed: ++closed; break;
          case QueuePostResult::invalid_callback: break;
        }
        if (j == 0) {
          first_posts.count_down();
        }
      }
    });
  }
  for (int i = 0; i < 2; ++i) {
    threads.emplace_back([&] {
      first_posts.wait();
      queue.join();
      ++stopped;
    });
  }
  ready.set_value();
  const bool closed_admission = wait_for_condition([&] { return !queue.status().accepting; });
  const auto draining = queue.status();
  const auto late = queue.post([] {});
  release.set_value();
  threads.clear();
  const auto done = queue.status();
  require(closed_admission && draining.queued > 0 && late == QueuePostResult::closed,
          "concurrent test did not exercise shutdown with accepted jobs pending");
  require(stopped == 2 && !done.accepting && done.queued == 0 && done.preparing == 0 &&
              done.executed == accepted + 1 &&
              done.rejected == full && accepted + full + closed == 2000, "concurrent stop lost work or counts");
}

}  // namespace

int main() {
  try {
    check_lifecycle();
    check_fifo_backpressure_and_exceptions();
    check_callable_lifecycle_can_reenter();
#ifdef _MSC_VER
    check_preparing_post_reserves_capacity();
#endif
    check_periodic_update_and_stop();
#ifdef __MINGW32__
    check_max_waits_do_not_spin();
#endif
    check_worker_exit_after_drain(false);
    check_worker_exit_after_drain(true);
    check_due_ticks_allow_jobs();
    check_concurrent_post_and_stop();
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
