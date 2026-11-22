#pragma once

#include <chrono>
#include <stop_token>
#include <string>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <winsock2.h>
#else
#include <sys/socket.h>
#endif

#include <orbit/rpc.hpp>

namespace orbit::detail {

#ifdef _WIN32
using Socket = SOCKET;
inline constexpr Socket invalid_socket = INVALID_SOCKET;
#else
using Socket = int;
inline constexpr Socket invalid_socket = -1;
#endif

using Clock = std::chrono::steady_clock;
using Deadline = Clock::time_point;
using Milliseconds = std::chrono::milliseconds;

class SocketRuntime {
 public:
  SocketRuntime();
  ~SocketRuntime();
  SocketRuntime(const SocketRuntime&) = delete;
  SocketRuntime& operator=(const SocketRuntime&) = delete;
};

class SocketHandle {
 public:
  explicit SocketHandle(Socket socket = invalid_socket) noexcept : socket_(socket) {}
  ~SocketHandle();
  SocketHandle(SocketHandle&& other) noexcept;
  SocketHandle& operator=(SocketHandle&& other) noexcept;

  Socket get() const noexcept { return socket_; }
  explicit operator bool() const noexcept { return socket_ != invalid_socket; }
  void shutdown() const noexcept;

 private:
  Socket socket_;
};

// Winsock select() needs a separate readable socket to interrupt a wait.
// POSIX poll() wakes when the connected socket is shut down.
class SocketInterrupt {
 public:
  SocketInterrupt();
  Socket get() const noexcept;
  void signal() const noexcept;

 private:
#ifdef _WIN32
  SocketHandle socket_;
#endif
};

void validate_timeout(Milliseconds timeout);
SocketHandle connect_tcp(const std::string& host, std::uint16_t port, Deadline deadline,
                         bool no_delay);
SocketHandle listen_tcp(const std::string& ip, std::uint16_t port, int backlog);
std::uint16_t bound_port(Socket listener);
SocketHandle accept_tcp(Socket listener, Milliseconds timeout, bool no_delay);

rpc::Frame read_frame(Socket socket, std::size_t limit, Deadline deadline, std::stop_token stop = {},
                      Socket interrupt = invalid_socket);
void write_all(Socket socket, std::span<const std::uint8_t> bytes, Deadline deadline,
               std::stop_token stop = {}, Socket interrupt = invalid_socket);

}  // namespace orbit::detail
