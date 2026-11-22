#pragma once

#include <chrono>
#include <memory>
#include <string>

#include <orbit/rpc.hpp>

namespace orbit {

struct TcpOptions {
  std::size_t max_payload = rpc::max_payload;
  // Address candidates share one connect deadline. System DNS lookup cannot be interrupted.
  // Sending and receiving each have a separate whole-operation timeout.
  std::chrono::milliseconds timeout{5000};
  // Set false to allow Nagle's algorithm on this connection.
  bool no_delay = true;
};

// Synchronous, plaintext TCP client. The system resolver interprets the host.
// Serialize access to each instance, including close and move operations.
class TcpClient {
 public:
  TcpClient(const std::string& host, std::uint16_t port, TcpOptions options = {});
  ~TcpClient();

  TcpClient(TcpClient&&) noexcept;
  TcpClient& operator=(TcpClient&&) noexcept;

  // Failures after I/O begins close the connection. A rejected local frame
  // leaves it open. Sending or receiving on a closed client throws.
  void send(const rpc::Frame& frame);
  rpc::Frame receive();

  // Reports local ownership of the socket, which can outlast peer disconnect.
  bool is_open() const noexcept;
  void close() noexcept;

 private:
  struct Connection;
  std::unique_ptr<Connection> connection_;
  TcpOptions options_;
};

}  // namespace orbit
