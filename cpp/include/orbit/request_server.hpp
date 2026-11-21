#pragma once

#include <chrono>
#include <exception>
#include <functional>
#include <memory>
#include <stop_token>
#include <string>
#include <unordered_map>

#include <orbit/rpc.hpp>

namespace orbit {

struct RequestReply {
  std::uint16_t id = 0;
  rpc::Bytes payload;
};

struct RequestServerOptions {
  std::size_t max_payload = rpc::max_payload;
  std::size_t max_connections = 64;
  // Server-wide bound on accepted work waiting for or executing a handler.
  std::size_t max_concurrent = 8;
  // Per-connection bound on buffered replies, excluding the active write.
  std::size_t send_queue = 32;
  std::chrono::milliseconds read_timeout{60000};
  std::chrono::milliseconds write_timeout{5000};
  // Zero gives one connection at most half the global capacity, rounded up.
  std::size_t max_pending_per_connection = 0;
  // Time one frame can wait for server-wide capacity while reads continue.
  std::chrono::milliseconds admission_timeout{1000};
  // Per-connection wire byte budget for buffered replies.
  std::size_t send_queue_bytes = 256 * 1024;
  // Set false to allow Nagle's algorithm on accepted connections.
  bool no_delay = true;
};

// Plaintext TCP server compatible with the default Go request.Client wire format.
// The constructor binds and starts the server. Port zero selects a free port.
class RequestServer {
 public:
  // Handlers can run concurrently. They receive a token that is canceled on
  // disconnect or shutdown. Return promptly when it is canceled.
  // Return a nonzero response ID declared by the caller; the server supplies
  // the original sequence. The input frame is valid for this invocation.
  using Handler = std::function<RequestReply(std::stop_token, const rpc::Frame&)>;
  using Handlers = std::unordered_map<std::uint16_t, Handler>;

  RequestServer(const std::string& ip, std::uint16_t port, Handlers handlers,
                RequestServerOptions options = {});
  ~RequestServer();
  RequestServer(const RequestServer&) = delete;
  RequestServer& operator=(const RequestServer&) = delete;

  std::uint16_t port() const noexcept;
  // Reports a terminal listener failure, or an empty exception_ptr.
  std::exception_ptr error() const;
  // Closes sessions and waits for all handlers to return. Idempotent.
  // Call from the owning thread, outside handlers and stop callbacks.
  void stop() noexcept;
  // Initiates the same shutdown and returns false if the deadline expires.
  // Keep this object alive after a timeout. Its destructor still joins handlers.
  bool stop_for(std::chrono::milliseconds timeout);

 private:
  struct Impl;
  std::unique_ptr<Impl> impl_;
};

}  // namespace orbit
