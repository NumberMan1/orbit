#pragma once

#include <chrono>
#include <cstddef>
#include <cstdint>
#include <exception>
#include <functional>
#include <memory>
#include <stop_token>

namespace orbit {

enum class QueueStartResult { started, closed, cancelled };
enum class QueuePostResult { accepted, closed, full, invalid_callback };

struct SerialQueueOptions {
  std::chrono::nanoseconds interval{};
  // Receives the scheduled tick on the monotonic clock.
  std::function<void(std::chrono::steady_clock::time_point)> update{};
  // Called on the worker after a callback throws. Exceptions from this hook are ignored.
  std::function<void(std::exception_ptr)> on_error{};
};

struct SerialQueueStatus {
  std::size_t queued = 0;
  std::size_t capacity = 0;
  bool accepting = false;
  // Includes periodic updates. Only full posts increment rejected.
  std::uint64_t executed = 0;
  std::uint64_t exceptions = 0;
  std::uint64_t rejected = 0;
  std::uint64_t execution_nanos = 0;
  // Slots reserved by concurrent posts before they publish their callbacks.
  std::size_t preparing = 0;
};

// Runs posted callbacks in FIFO order on one worker, interleaved with optional updates.
// Operations can run concurrently while the queue is alive. Stop, join and
// destruction belong outside callbacks.
class SerialQueue {
 public:
  using Callback = std::function<void()>;

  // Capacity must be positive and counts preparing posts and waiting callbacks.
  // The interval must be nonnegative, or positive when an update is supplied.
  // Invalid options throw std::invalid_argument.
  explicit SerialQueue(std::ptrdiff_t capacity, SerialQueueOptions options = {});
  ~SerialQueue();
  SerialQueue(const SerialQueue&) = delete;
  SerialQueue& operator=(const SerialQueue&) = delete;

  // Starts once. The token is checked only during startup.
  QueueStartResult start(std::stop_token token = {});
  // Returns immediately on full or closed. Safe to call from a callback.
  QueuePostResult post(Callback callback);
  // Closes admission, disables further ticks and drains accepted work. Idempotent.
  // Returns true after callbacks have drained.
  // Cancellation or timeout during drain returns false and leaves it running.
  // Keep the queue and borrowed callback state alive until it has drained.
  // Calling a stop method from a callback throws std::logic_error.
  bool stop(std::stop_token token = {});
  bool stop_for(std::chrono::nanoseconds timeout);
  // Initiates shutdown, waits for worker exit and releases its thread resources.
  // Safe to repeat or call concurrently. The destructor also joins the worker.
  void join();
  SerialQueueStatus status() const;

 private:
  struct Impl;
  std::unique_ptr<Impl> impl_;
};

}  // namespace orbit
