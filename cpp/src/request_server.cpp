#include "tcp_transport.hpp"

#include <orbit/request_server.hpp>

#include <atomic>
#include <climits>
#include <condition_variable>
#include <deque>
#include <list>
#include <mutex>
#include <stdexcept>
#include <thread>
#include <unordered_set>
#include <utility>
#include <vector>

#include "rpc_header.hpp"

namespace orbit {
namespace {

RequestServerOptions validate_options(RequestServerOptions options,
                                      const RequestServer::Handlers& handlers) {
  rpc::detail::validate_limit(options.max_payload);
  detail::validate_timeout(options.read_timeout);
  detail::validate_timeout(options.write_timeout);
  detail::validate_timeout(options.admission_timeout);
  if (options.max_connections == 0 || options.max_connections > INT_MAX ||
      options.max_concurrent == 0 || options.send_queue == 0 || handlers.empty() ||
      options.max_pending_per_connection > options.max_concurrent ||
      options.send_queue_bytes < rpc::header_size + options.max_payload) {
    throw std::invalid_argument("orbit: invalid request server limits or handlers");
  }
  if (options.max_pending_per_connection == 0) {
    options.max_pending_per_connection = options.max_concurrent - options.max_concurrent / 2;
  }
  for (const auto& [id, handler] : handlers) {
    if (id == 0 || !handler) {
      throw std::invalid_argument("orbit: invalid request server handler");
    }
  }
  return options;
}

struct ConnectionIO {
  explicit ConnectionIO(detail::SocketHandle handle) : socket(std::move(handle)) {}

  void shutdown() const noexcept {
    socket.shutdown();
    interrupt.signal();
  }

  detail::SocketHandle socket;
  [[no_unique_address]] detail::SocketInterrupt interrupt;
};

class Session {
 public:
  explicit Session(ConnectionIO& io, const RequestServerOptions& options)
      : io_(&io), options_(options) {}

  rpc::Frame receive(const ConnectionIO& io) const {
    return detail::read_frame(io.socket.get(), options_.max_payload,
                              detail::Clock::now() + options_.read_timeout, token(),
                              io.interrupt.get());
  }

  void enqueue(rpc::Bytes reply) {
    {
      std::lock_guard lock(mutex_);
      if (closed_) {
        return;
      }
      if (replies_.size() == options_.send_queue ||
          reply.size() > options_.send_queue_bytes - queued_bytes_) {
        throw std::runtime_error("orbit: TCP reply queue full");
      }
      replies_.push_back(std::move(reply));
      queued_bytes_ += replies_.back().size();
    }
    ready_.notify_one();
  }

  void write_replies(const ConnectionIO& io) noexcept {
    try {
      for (auto reply = next_reply(); !reply.empty(); reply = next_reply()) {
        detail::write_all(io.socket.get(), reply, detail::Clock::now() + options_.write_timeout,
                          token(), io.interrupt.get());
      }
    } catch (...) {
      close();
    }
  }

  bool closed() const noexcept { return closed_; }
  std::stop_token token() const noexcept { return stop_.get_token(); }

  // Protected by the server's request-queue mutex.
  std::unordered_set<std::uint32_t> active_sequences;
  bool waiting_for_capacity = false;

  void shutdown_io() noexcept {
    {
      std::lock_guard lock(mutex_);
      if (closed_.exchange(true)) {
        return;
      }
      replies_.clear();
      queued_bytes_ = 0;
      if (io_) {
        io_->shutdown();
      }
    }
    ready_.notify_one();
  }

  void close() noexcept {
    shutdown_io();
    stop_.request_stop();
  }

  void detach_io() noexcept {
    std::lock_guard lock(mutex_);
    io_ = nullptr;
  }

 private:
  rpc::Bytes next_reply() {
    std::unique_lock lock(mutex_);
    ready_.wait(lock, [this] { return closed_ || !replies_.empty(); });
    if (closed_) {
      return {};
    }
    auto reply = std::move(replies_.front());
    replies_.pop_front();
    queued_bytes_ -= reply.size();
    return reply;
  }

  ConnectionIO* io_;  // Borrowed from Connection under mutex_.
  const RequestServerOptions options_;
  std::atomic<bool> closed_{false};
  std::stop_source stop_;
  std::mutex mutex_;
  std::condition_variable ready_;
  std::deque<rpc::Bytes> replies_;
  std::size_t queued_bytes_ = 0;
};

// Only the accept thread owns the sockets and joins the I/O threads.
struct Connection {
  Connection(detail::SocketHandle socket, const RequestServerOptions& options)
      : io(std::move(socket)), session(std::make_shared<Session>(io, options)) {}

  ~Connection() {
    session->shutdown_io();
    if (reader.joinable()) {
      reader.join();
    }
    if (writer.joinable()) {
      writer.join();
    }
    session->detach_io();
  }

  ConnectionIO io;
  std::shared_ptr<Session> session;
  std::jthread reader;
  std::jthread writer;
  std::atomic<unsigned> finished{0};
};

struct Work {
  std::shared_ptr<Session> session;
  const RequestServer::Handler* handler;
  rpc::Frame frame;
  detail::Deadline deadline{};
};

using WorkQueue = std::list<Work>;

}  // namespace

struct RequestServer::Impl {
  Impl(const std::string& ip, std::uint16_t port, Handlers handlers,
       RequestServerOptions options)
      : options(validate_options(options, handlers)),
        handlers(std::move(handlers)),
        listener(detail::listen_tcp(ip, port, static_cast<int>(options.max_connections))),
        port(detail::bound_port(listener.get())) {
    workers.reserve(options.max_concurrent);
    try {
      for (std::size_t i = 0; i < options.max_concurrent; ++i) {
        workers.emplace_back([this] { run_worker(); });
      }
      accept_thread = std::jthread([this] { accept_connections(); });
    } catch (...) {
      stop();
      throw;
    }
  }

  ~Impl() { stop(); }

  void stop() noexcept {
    request_stop();
    join_threads();
  }

  bool stop_for(std::chrono::milliseconds timeout) {
    request_stop();
    {
      std::unique_lock lock(mutex);
      if (!stopped.wait_for(lock, timeout, [this] { return accept_done && workers_remaining == 0; })) {
        return false;
      }
    }
    join_threads();
    return true;
  }

  void join_threads() noexcept {
    if (accept_thread.joinable()) {
      accept_thread.join();
    }
    listener = detail::SocketHandle{};
    workers.clear();
  }

  std::exception_ptr error() const {
    std::lock_guard lock(mutex);
    return listener_error;
  }

  void request_stop() noexcept {
    {
      std::lock_guard lock(mutex);
      stopping = true;
      in_flight -= requests.size();
      for (const auto& work : requests) {
        work.session->active_sequences.erase(work.frame.sequence);
      }
      for (const auto& work : waiting) {
        work.session->active_sequences.erase(work.frame.sequence);
        work.session->waiting_for_capacity = false;
      }
      requests.clear();
      waiting.clear();
    }
    available.notify_all();
  }

  void accept_connections() noexcept {
    std::vector<std::unique_ptr<Connection>> connections;
    try {
      while (!stopping) {
        auto socket = detail::accept_tcp(listener.get(), std::chrono::milliseconds{50},
                                         options.no_delay);
        expire_waiting();
        std::erase_if(connections, [](const auto& conn) { return conn->finished == 2; });
        if (!socket || stopping || connections.size() >= options.max_connections) {
          continue;
        }
        try {
          auto connection = std::make_unique<Connection>(std::move(socket), options);
          auto* conn = connection.get();
          conn->writer = std::jthread([conn] {
            conn->session->write_replies(conn->io);
            ++conn->finished;
          });
          conn->reader = std::jthread([this, conn] {
            read_requests(*conn);
            ++conn->finished;
          });
          connections.push_back(std::move(connection));
        } catch (const std::system_error&) {
          // Failure to configure one connection or start its threads is local.
          std::this_thread::sleep_for(std::chrono::milliseconds{50});
        }
      }
    } catch (...) {
      std::lock_guard lock(mutex);
      listener_error = std::current_exception();
    }
    request_stop();
    listener = detail::SocketHandle{};
    for (const auto& connection : connections) {
      connection->session->shutdown_io();
    }
    connections.clear();
    {
      std::lock_guard lock(mutex);
      accept_done = true;
    }
    stopped.notify_all();
  }

  void read_requests(const Connection& conn) noexcept {
    const auto& session = conn.session;
    try {
      while (!session->closed()) {
        auto frame = session->receive(conn.io);
        const auto handler = handlers.find(frame.id);
        if (frame.sequence == 0 || handler == handlers.end() ||
            !submit({session, &handler->second, std::move(frame)})) {
          break;
        }
      }
    } catch (...) {
      // An invalid or incomplete frame closes only this connection.
    }
    session->close();
    {
      std::lock_guard lock(mutex);
      const auto discard = [&session](const Work& work) {
        if (work.session != session) {
          return false;
        }
        session->active_sequences.erase(work.frame.sequence);
        return true;
      };
      in_flight -= std::erase_if(requests, discard);
      std::erase_if(waiting, discard);
      session->waiting_for_capacity = false;
      admit_waiting();
    }
  }

  bool submit(Work work) {
    std::lock_guard lock(mutex);
    auto& session = *work.session;
    if (stopping || session.closed() || session.waiting_for_capacity ||
        session.active_sequences.size() >= options.max_pending_per_connection ||
        !session.active_sequences.insert(work.frame.sequence).second) {
      return false;
    }
    if (in_flight == options.max_concurrent || !waiting.empty()) {
      work.deadline = detail::Clock::now() + options.admission_timeout;
      waiting.push_back(std::move(work));
      session.waiting_for_capacity = true;
    } else {
      requests.push_back(std::move(work));
      ++in_flight;
      available.notify_one();
    }
    return true;
  }

  // The caller holds mutex. A list splice moves a waiting frame without allocation.
  void admit_waiting() noexcept {
    while (!stopping && in_flight < options.max_concurrent && !waiting.empty()) {
      auto& work = waiting.front();
      if (work.session->closed()) {
        work.session->active_sequences.erase(work.frame.sequence);
        work.session->waiting_for_capacity = false;
        waiting.pop_front();
        continue;
      }
      if (work.deadline <= detail::Clock::now()) {
        return;  // The accept loop closes expired sessions outside mutex.
      }
      work.session->waiting_for_capacity = false;
      requests.splice(requests.end(), waiting, waiting.begin());
      ++in_flight;
      available.notify_one();
    }
  }

  void expire_waiting() {
    WorkQueue expired;
    {
      const auto now = detail::Clock::now();
      std::lock_guard lock(mutex);
      while (!waiting.empty() && waiting.front().deadline <= now) {
        auto& work = waiting.front();
        work.session->active_sequences.erase(work.frame.sequence);
        // Keep waiting_for_capacity set until close so a new frame cannot take this slot.
        expired.splice(expired.end(), waiting, waiting.begin());
      }
      admit_waiting();
    }
    for (const auto& work : expired) {
      work.session->shutdown_io();
    }
  }

  void run_worker() noexcept {
    for (;;) {
      std::unique_lock lock(mutex);
      available.wait(lock, [this] { return stopping || !requests.empty(); });
      if (stopping) {
        --workers_remaining;
        lock.unlock();
        stopped.notify_all();
        return;
      }
      auto work = std::move(requests.front());
      requests.pop_front();
      lock.unlock();

      rpc::Bytes wire;
      try {
        if (!work.session->closed()) {
          auto reply = (*work.handler)(work.session->token(), work.frame);
          if (reply.id == 0) {
            throw std::invalid_argument("orbit: reply ID must be nonzero");
          }
          wire = rpc::marshal({reply.id, work.frame.sequence, std::move(reply.payload)},
                              options.max_payload);
        }
      } catch (...) {
        work.session->close();
      }
      bool overflow = false;
      {
        std::lock_guard guard(mutex);
        // Hold admission until enqueue returns, so a new request cannot race
        // publication of the preceding response with release of its slot.
        if (!wire.empty()) {
          try {
            work.session->enqueue(std::move(wire));
          } catch (...) {
            overflow = true;
          }
        }
        work.session->active_sequences.erase(work.frame.sequence);
        --in_flight;
        if (!overflow) {
          admit_waiting();
        }
      }
      if (overflow) {
        work.session->close();
      }
    }
  }

  const RequestServerOptions options;
  const Handlers handlers;
  detail::SocketRuntime runtime;
  detail::SocketHandle listener;
  const std::uint16_t port;
  mutable std::mutex mutex;
  std::condition_variable available;
  std::condition_variable stopped;
  std::atomic<bool> stopping{false};
  std::size_t in_flight = 0;
  std::size_t workers_remaining = options.max_concurrent;
  bool accept_done = false;
  WorkQueue requests;
  WorkQueue waiting;
  std::exception_ptr listener_error;
  std::vector<std::jthread> workers;
  std::jthread accept_thread;
};

RequestServer::RequestServer(const std::string& ip, std::uint16_t port, Handlers handlers,
                             RequestServerOptions options)
    : impl_(std::make_unique<Impl>(ip, port, std::move(handlers), options)) {}

RequestServer::~RequestServer() = default;

std::uint16_t RequestServer::port() const noexcept { return impl_->port; }
std::exception_ptr RequestServer::error() const { return impl_->error(); }
void RequestServer::stop() noexcept { impl_->stop(); }
bool RequestServer::stop_for(std::chrono::milliseconds timeout) { return impl_->stop_for(timeout); }

}  // namespace orbit
