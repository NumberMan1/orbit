#include "tcp_transport.hpp"

#include <array>
#include <climits>
#include <exception>
#include <memory>
#include <stdexcept>
#include <system_error>
#include <thread>
#include <utility>

#include "rpc_header.hpp"

#ifdef _WIN32
#include <windows.h>
#include <ws2tcpip.h>
#else
#include <cerrno>
#include <fcntl.h>
#include <netdb.h>
#include <poll.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <unistd.h>
#endif

namespace orbit::detail {
namespace {

#ifdef _WIN32
using SocketLength = int;
constexpr int interrupted = WSAEINTR;
constexpr int send_flags = 0;

int socket_error() { return WSAGetLastError(); }
bool would_block(int error) { return error == WSAEWOULDBLOCK; }
bool connect_pending(int error) { return would_block(error); }
#else
using SocketLength = socklen_t;
constexpr int interrupted = EINTR;
#ifdef SO_NOSIGPIPE
constexpr int send_flags = 0;
#else
constexpr int send_flags = MSG_NOSIGNAL;
#endif

int socket_error() { return errno; }
bool would_block(int error) { return error == EAGAIN || error == EWOULDBLOCK; }
bool connect_pending(int error) { return error == EINPROGRESS || error == interrupted; }
#endif

[[noreturn]] void fail_socket(const char* operation, int error = socket_error()) {
  throw std::system_error(error, std::system_category(), operation);
}

void set_tcp_no_delay(Socket socket, bool no_delay) {
  const int value = no_delay ? 1 : 0;
  if (::setsockopt(socket, IPPROTO_TCP, TCP_NODELAY, reinterpret_cast<const char*>(&value),
                   sizeof(value)) != 0) {
    fail_socket("orbit: setsockopt TCP_NODELAY");
  }
}

void set_nonblocking(Socket socket) {
#ifdef _WIN32
  unsigned long enabled = 1;
  if (::ioctlsocket(socket, FIONBIO, &enabled) != 0) {
    fail_socket("orbit: ioctlsocket");
  }
#else
  const int flags = ::fcntl(socket, F_GETFL, 0);
  if (flags < 0 || ::fcntl(socket, F_SETFL, flags | O_NONBLOCK) < 0) {
    fail_socket("orbit: fcntl");
  }
#ifdef SO_NOSIGPIPE
  const int enabled = 1;
  if (::setsockopt(socket, SOL_SOCKET, SO_NOSIGPIPE, &enabled, sizeof(enabled)) != 0) {
    fail_socket("orbit: setsockopt SO_NOSIGPIPE");
  }
#endif
#endif
}

void prevent_inheritance(Socket socket) {
#ifdef _WIN32
  if (!::SetHandleInformation(reinterpret_cast<HANDLE>(socket), HANDLE_FLAG_INHERIT, 0)) {
    fail_socket("orbit: SetHandleInformation", static_cast<int>(::GetLastError()));
  }
#elif !defined(__linux__)
  if (::fcntl(socket, F_SETFD, FD_CLOEXEC) < 0) {
    fail_socket("orbit: fcntl FD_CLOEXEC");
  }
#else
  // Linux sets close-on-exec when allocating the descriptor.
  (void)socket;
#endif
}

enum class WaitFor { read, write, connect };

[[noreturn]] void interrupted_wait() {
  throw std::system_error(std::make_error_code(std::errc::operation_canceled), "orbit: TCP stopped");
}

bool poll_ready(Socket socket, WaitFor wait, Milliseconds interval,
                Socket interrupt = invalid_socket) {
  const auto milliseconds = interval.count();
#ifdef _WIN32
  fd_set reads;
  fd_set writes;
  fd_set errors;
  FD_ZERO(&reads);
  FD_ZERO(&writes);
  FD_ZERO(&errors);
  if (wait == WaitFor::read) {
    FD_SET(socket, &reads);
  } else {
    FD_SET(socket, &writes);
  }
  if (interrupt != invalid_socket) {
    FD_SET(interrupt, &reads);
  }
  FD_SET(socket, &errors);
  timeval timeout{static_cast<long>(milliseconds / 1000),
                  static_cast<long>((milliseconds % 1000) * 1000)};
  // Winsock reports a failed asynchronous connect through exceptfds.
  fd_set* except = wait == WaitFor::connect ? &errors : nullptr;
  const int result = ::select(0, reads.fd_count ? &reads : nullptr,
                              writes.fd_count ? &writes : nullptr, except, &timeout);
  if (result > 0 && interrupt != invalid_socket && FD_ISSET(interrupt, &reads)) {
    interrupted_wait();
  }
#else
  (void)interrupt;
  pollfd descriptor{socket, static_cast<short>(wait == WaitFor::read ? POLLIN : POLLOUT), 0};
  const int result = ::poll(&descriptor, 1, static_cast<int>(milliseconds));
#endif
  if (result < 0) {
    const int error = socket_error();
    if (error != interrupted) {
      fail_socket("orbit: wait for TCP", error);
    }
  }
  return result > 0;
}

void check_stop(std::stop_token stop) {
  if (stop.stop_requested()) {
    interrupted_wait();
  }
}

Clock::duration remaining_time(Deadline deadline, const char* timeout_message, std::stop_token stop) {
  check_stop(stop);
  const auto remaining = deadline - Clock::now();
  if (remaining <= Clock::duration::zero()) {
    throw std::system_error(std::make_error_code(std::errc::timed_out), timeout_message);
  }
  return remaining;
}

void wait_ready(Socket socket, WaitFor wait, Deadline deadline, const char* timeout_message,
                std::stop_token stop = {}, Socket interrupt = invalid_socket) {
  for (;;) {
    const auto remaining = remaining_time(deadline, timeout_message, stop);
    if (poll_ready(socket, wait, std::chrono::ceil<Milliseconds>(remaining), interrupt)) {
      remaining_time(deadline, timeout_message, stop);
      return;
    }
  }
}

void connect_socket(Socket socket, const addrinfo& address, Deadline deadline) {
  if (::connect(socket, address.ai_addr, static_cast<SocketLength>(address.ai_addrlen)) == 0) {
    return;
  }
  const int error = socket_error();
  if (!connect_pending(error)) {
    fail_socket("orbit: TCP connect", error);
  }
  wait_ready(socket, WaitFor::connect, deadline, "orbit: TCP connect timed out");
  int status = 0;
  SocketLength length = sizeof(status);
  if (::getsockopt(socket, SOL_SOCKET, SO_ERROR, reinterpret_cast<char*>(&status), &length) != 0) {
    fail_socket("orbit: getsockopt SO_ERROR");
  }
  if (status != 0) {
    fail_socket("orbit: TCP connect", status);
  }
}

void read_all(Socket socket, std::span<std::uint8_t> bytes, Deadline deadline, std::stop_token stop,
              Socket interrupt) {
  constexpr const char* timeout = "orbit: TCP read timed out";
  while (!bytes.empty()) {
    remaining_time(deadline, timeout, stop);
    const auto count = ::recv(socket, reinterpret_cast<char*>(bytes.data()),
                              static_cast<int>(bytes.size()), 0);
    if (count < 0) {
      const int error = socket_error();
      if (would_block(error)) {
        wait_ready(socket, WaitFor::read, deadline, timeout, stop, interrupt);
        continue;
      }
      if (error == interrupted) {
        continue;
      }
      fail_socket("orbit: TCP read", error);
    }
    if (count == 0) {
      throw std::runtime_error("orbit: TCP peer closed while reading a frame");
    }
    bytes = bytes.subspan(static_cast<std::size_t>(count));
  }
}

using Address = std::unique_ptr<addrinfo, decltype(&::freeaddrinfo)>;

Address resolve_addresses(const std::string& host, std::uint16_t port, bool numeric_only) {
  // Reject NUL so getaddrinfo cannot resolve a truncated input.
  if (host.empty() || host.contains('\0')) {
    throw std::invalid_argument("orbit: invalid TCP host");
  }
  addrinfo hints{};
  hints.ai_family = AF_UNSPEC;
  hints.ai_socktype = SOCK_STREAM;
  hints.ai_protocol = IPPROTO_TCP;
  hints.ai_flags = AI_NUMERICSERV | (numeric_only ? AI_NUMERICHOST : 0);
  addrinfo* raw_address = nullptr;
  const auto service = std::to_string(port);
  const int error = ::getaddrinfo(host.c_str(), service.c_str(), &hints, &raw_address);
  if (error != 0) {
    if (numeric_only) {
      throw std::invalid_argument("orbit: expected a numeric IPv4 or IPv6 address");
    }
    throw std::runtime_error("orbit: TCP host resolution failed (getaddrinfo " +
                             std::to_string(error) + ")");
  }
  return Address(raw_address, ::freeaddrinfo);
}

SocketHandle open_socket(const addrinfo& address) {
#ifdef _WIN32
  SocketHandle socket(::WSASocketW(address.ai_family, address.ai_socktype, address.ai_protocol,
                                   nullptr, 0, WSA_FLAG_OVERLAPPED | WSA_FLAG_NO_HANDLE_INHERIT));
#elif defined(__linux__)
  SocketHandle socket(::socket(address.ai_family, address.ai_socktype | SOCK_CLOEXEC,
                                address.ai_protocol));
#else
  SocketHandle socket(::socket(address.ai_family, address.ai_socktype, address.ai_protocol));
#endif
  if (!socket) {
    fail_socket("orbit: TCP socket");
  }
#ifndef _WIN32
  prevent_inheritance(socket.get());
#endif
  set_nonblocking(socket.get());
  return socket;
}

void close_socket(Socket socket) noexcept {
  if (socket == invalid_socket) {
    return;
  }
#ifdef _WIN32
  ::closesocket(socket);
#else
  ::close(socket);
#endif
}

}  // namespace

SocketRuntime::SocketRuntime() {
#ifdef _WIN32
  WSADATA data{};
  const int error = ::WSAStartup(MAKEWORD(2, 2), &data);
  if (error != 0) {
    fail_socket("orbit: WSAStartup", error);
  }
#endif
}

SocketRuntime::~SocketRuntime() {
#ifdef _WIN32
  ::WSACleanup();
#endif
}

SocketHandle::~SocketHandle() { close_socket(socket_); }

SocketHandle::SocketHandle(SocketHandle&& other) noexcept
    : socket_(std::exchange(other.socket_, invalid_socket)) {}

SocketHandle& SocketHandle::operator=(SocketHandle&& other) noexcept {
  if (this != &other) {
    close_socket(socket_);
    socket_ = std::exchange(other.socket_, invalid_socket);
  }
  return *this;
}

void SocketHandle::shutdown() const noexcept {
#ifdef _WIN32
  ::shutdown(socket_, SD_BOTH);
#else
  ::shutdown(socket_, SHUT_RDWR);
#endif
}

SocketInterrupt::SocketInterrupt() {
#ifdef _WIN32
  socket_ = SocketHandle(::WSASocketW(AF_INET, SOCK_DGRAM, IPPROTO_UDP, nullptr, 0,
                                      WSA_FLAG_OVERLAPPED | WSA_FLAG_NO_HANDLE_INHERIT));
  if (!socket_) {
    fail_socket("orbit: TCP interrupt socket");
  }
  sockaddr_in address{};
  address.sin_family = AF_INET;
  address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  SocketLength length = sizeof(address);
  auto* endpoint = reinterpret_cast<sockaddr*>(&address);
  if (::bind(socket_.get(), endpoint, length) != 0 ||
      ::getsockname(socket_.get(), endpoint, &length) != 0 ||
      ::connect(socket_.get(), endpoint, length) != 0) {
    fail_socket("orbit: configure TCP interrupt socket");
  }
  set_nonblocking(socket_.get());
#endif
}

Socket SocketInterrupt::get() const noexcept {
#ifdef _WIN32
  return socket_.get();
#else
  return invalid_socket;
#endif
}

void SocketInterrupt::signal() const noexcept {
#ifdef _WIN32
  // Leave the datagram unread so both the reader and writer can observe it.
  const char signal = 1;
  ::send(socket_.get(), &signal, 1, 0);
#endif
}

void validate_timeout(Milliseconds timeout) {
  if (timeout <= Milliseconds::zero() || timeout > Milliseconds{INT_MAX}) {
    throw std::invalid_argument("orbit: TCP timeout outside supported range");
  }
}

SocketHandle connect_tcp(const std::string& host, std::uint16_t port, Deadline deadline,
                         bool no_delay) {
  const auto addresses = resolve_addresses(host, port, false);
  Clock::duration::rep candidates = 0;
  for (auto* address = addresses.get(); address; address = address->ai_next) {
    ++candidates;
  }
  std::exception_ptr failure;
  // Try candidates only while establishing the connection. The caller owns all I/O retries.
  for (auto* address = addresses.get(); address; address = address->ai_next) {
    const auto remaining = remaining_time(deadline, "orbit: TCP connect timed out", {});
    // A stalled first address should leave time to try the other addresses.
    const auto attempt_deadline = deadline - remaining + remaining / candidates;
    --candidates;
    try {
      auto socket = open_socket(*address);
      connect_socket(socket.get(), *address, attempt_deadline);
      set_tcp_no_delay(socket.get(), no_delay);
      return socket;
    } catch (const std::system_error&) {
      failure = std::current_exception();
    }
  }
  if (failure) {
    std::rethrow_exception(failure);
  }
  throw std::runtime_error("orbit: TCP host has no usable address");
}

SocketHandle listen_tcp(const std::string& ip, std::uint16_t port, int backlog) {
  const auto address = resolve_addresses(ip, port, true);
  auto socket = open_socket(*address);
  const int enabled = 1;
#ifdef _WIN32
  const int option = SO_EXCLUSIVEADDRUSE;
#else
  const int option = SO_REUSEADDR;
#endif
  if (::setsockopt(socket.get(), SOL_SOCKET, option, reinterpret_cast<const char*>(&enabled),
                   sizeof(enabled)) != 0) {
    fail_socket("orbit: TCP listener address options");
  }
  if (::bind(socket.get(), address->ai_addr, static_cast<SocketLength>(address->ai_addrlen)) != 0) {
    fail_socket("orbit: TCP bind");
  }
  if (::listen(socket.get(), backlog) != 0) {
    fail_socket("orbit: TCP listen");
  }
  return socket;
}

std::uint16_t bound_port(Socket listener) {
  sockaddr_storage address{};
  SocketLength length = sizeof(address);
  if (::getsockname(listener, reinterpret_cast<sockaddr*>(&address), &length) != 0) {
    fail_socket("orbit: TCP getsockname");
  }
  if (address.ss_family == AF_INET6) {
    return ntohs(reinterpret_cast<const sockaddr_in6*>(&address)->sin6_port);
  }
  return ntohs(reinterpret_cast<const sockaddr_in*>(&address)->sin_port);
}

SocketHandle accept_tcp(Socket listener, Milliseconds timeout, bool no_delay) {
  if (!poll_ready(listener, WaitFor::read, timeout)) {
    return SocketHandle{};
  }
#ifdef __linux__
  SocketHandle socket(::accept4(listener, nullptr, nullptr, SOCK_CLOEXEC));
#else
  SocketHandle socket(::accept(listener, nullptr, nullptr));
#endif
  if (!socket) {
    const int error = socket_error();
#ifdef _WIN32
    constexpr int aborted = WSAECONNRESET;
    const bool pressure = error == WSAEMFILE || error == WSAENOBUFS;
#else
    constexpr int aborted = ECONNABORTED;
    const bool pressure = error == EMFILE || error == ENFILE || error == ENOBUFS || error == ENOMEM;
#endif
    if (pressure) {
      // The listener is still usable. Give other threads time to release resources.
      std::this_thread::sleep_for(timeout);
      return socket;
    }
#ifdef __linux__
    // Linux can surface a pending socket's network error through accept().
    if (error == ENETDOWN || error == EPROTO || error == ENOPROTOOPT || error == EHOSTDOWN ||
        error == ENONET || error == EHOSTUNREACH || error == EOPNOTSUPP || error == ENETUNREACH) {
      return socket;
    }
#endif
    if (would_block(error) || error == interrupted || error == aborted) {
      return socket;
    }
    fail_socket("orbit: TCP accept", error);
  }
  try {
    prevent_inheritance(socket.get());
    set_nonblocking(socket.get());
    set_tcp_no_delay(socket.get(), no_delay);
  } catch (const std::system_error&) {
    return SocketHandle{};
  }
  return socket;
}

rpc::Frame read_frame(Socket socket, std::size_t limit, Deadline deadline, std::stop_token stop,
                      Socket interrupt) {
  std::array<std::uint8_t, rpc::header_size> bytes{};
  read_all(socket, bytes, deadline, stop, interrupt);
  const auto header = rpc::detail::read_header(bytes.data(), limit);
  rpc::Frame frame{header.id, header.sequence, rpc::Bytes(header.length)};
  read_all(socket, frame.payload, deadline, stop, interrupt);
  return frame;
}

void write_all(Socket socket, std::span<const std::uint8_t> bytes, Deadline deadline,
               std::stop_token stop, Socket interrupt) {
  constexpr const char* timeout = "orbit: TCP write timed out";
  while (!bytes.empty()) {
    remaining_time(deadline, timeout, stop);
    const auto count = ::send(socket, reinterpret_cast<const char*>(bytes.data()),
                              static_cast<int>(bytes.size()), send_flags);
    if (count < 0) {
      const int error = socket_error();
      if (would_block(error)) {
        wait_ready(socket, WaitFor::write, deadline, timeout, stop, interrupt);
        continue;
      }
      if (error == interrupted) {
        continue;
      }
      fail_socket("orbit: TCP write", error);
    }
    if (count == 0) {
      throw std::runtime_error("orbit: TCP write made no progress");
    }
    bytes = bytes.subspan(static_cast<std::size_t>(count));
  }
}

}  // namespace orbit::detail
