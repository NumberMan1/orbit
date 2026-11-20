#include <orbit/tcp_client.hpp>

#include <array>
#include <climits>
#include <stdexcept>
#include <system_error>

#include "rpc_header.hpp"

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <cerrno>
#include <fcntl.h>
#include <netdb.h>
#include <poll.h>
#include <sys/socket.h>
#include <unistd.h>
#endif

namespace orbit {
namespace {

using Clock = std::chrono::steady_clock;
using Deadline = Clock::time_point;
using Milliseconds = std::chrono::milliseconds;

#ifdef _WIN32
using Socket = SOCKET;
using SocketLength = int;
constexpr Socket invalid_socket = INVALID_SOCKET;
constexpr int interrupted = WSAEINTR;
constexpr int send_flags = 0;

int socket_error() { return WSAGetLastError(); }
bool would_block(int error) { return error == WSAEWOULDBLOCK; }
bool connect_pending(int error) { return would_block(error); }
#else
using Socket = int;
using SocketLength = socklen_t;
constexpr Socket invalid_socket = -1;
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

enum class WaitFor { read, write, connect };

void wait_ready(Socket socket, WaitFor wait, Deadline deadline, const char* timeout_message) {
  for (;;) {
    const auto remaining = deadline - Clock::now();
    if (remaining <= Clock::duration::zero()) {
      throw std::system_error(std::make_error_code(std::errc::timed_out), timeout_message);
    }
    const auto milliseconds = std::chrono::ceil<Milliseconds>(remaining).count();
#ifdef _WIN32
    fd_set ready;
    fd_set errors;
    FD_ZERO(&ready);
    FD_ZERO(&errors);
    FD_SET(socket, &ready);
    FD_SET(socket, &errors);
    timeval timeout{static_cast<long>(milliseconds / 1000),
                    static_cast<long>((milliseconds % 1000) * 1000)};
    // Winsock reports a failed asynchronous connect through exceptfds.
    fd_set* except = wait == WaitFor::connect ? &errors : nullptr;
    const int result = wait == WaitFor::read
                           ? ::select(0, &ready, nullptr, nullptr, &timeout)
                           : ::select(0, nullptr, &ready, except, &timeout);
#else
    pollfd descriptor{socket, static_cast<short>(wait == WaitFor::read ? POLLIN : POLLOUT), 0};
    const int result = ::poll(&descriptor, 1, static_cast<int>(milliseconds));
#endif
    if (result > 0) {
      return;
    }
    if (result < 0) {
      const int error = socket_error();
      if (error != interrupted) {
        fail_socket("orbit: wait for TCP", error);
      }
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

void write_all(Socket socket, std::span<const std::uint8_t> bytes, Deadline deadline) {
  while (!bytes.empty()) {
    wait_ready(socket, WaitFor::write, deadline, "orbit: TCP write timed out");
    const auto count = ::send(socket, reinterpret_cast<const char*>(bytes.data()),
                              static_cast<int>(bytes.size()), send_flags);
    if (count < 0) {
      const int error = socket_error();
      if (error == interrupted || would_block(error)) {
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

void read_all(Socket socket, std::span<std::uint8_t> bytes, Deadline deadline) {
  while (!bytes.empty()) {
    wait_ready(socket, WaitFor::read, deadline, "orbit: TCP read timed out");
    const auto count = ::recv(socket, reinterpret_cast<char*>(bytes.data()),
                              static_cast<int>(bytes.size()), 0);
    if (count < 0) {
      const int error = socket_error();
      if (error == interrupted || would_block(error)) {
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

}  // namespace

struct TcpClient::Connection {
  Socket socket = invalid_socket;

  Connection() {
#ifdef _WIN32
    WSADATA data{};
    const int error = ::WSAStartup(MAKEWORD(2, 2), &data);
    if (error != 0) {
      fail_socket("orbit: WSAStartup", error);
    }
#endif
  }

  ~Connection() {
#ifdef _WIN32
    if (socket != invalid_socket) {
      ::closesocket(socket);
    }
    ::WSACleanup();
#else
    if (socket != invalid_socket) {
      ::close(socket);
    }
#endif
  }
};

TcpClient::TcpClient(const std::string& ip, std::uint16_t port, TcpOptions options)
    : options_(options) {
  rpc::detail::validate_limit(options.max_payload);
  if (options.timeout <= Milliseconds::zero() || options.timeout > Milliseconds{INT_MAX}) {
    throw std::invalid_argument("orbit: TCP timeout outside supported range");
  }
  if (ip.empty() || ip.find('\0') != std::string::npos || port == 0) {
    throw std::invalid_argument("orbit: expected a numeric IP and a nonzero port");
  }
  const auto deadline = Clock::now() + options.timeout;
  connection_ = std::make_unique<Connection>();
  addrinfo hints{};
  hints.ai_family = AF_UNSPEC;
  hints.ai_socktype = SOCK_STREAM;
  hints.ai_protocol = IPPROTO_TCP;
  hints.ai_flags = AI_NUMERICHOST | AI_NUMERICSERV;
  addrinfo* raw_address = nullptr;
  const auto service = std::to_string(port);
  if (::getaddrinfo(ip.c_str(), service.c_str(), &hints, &raw_address) != 0) {
    throw std::invalid_argument("orbit: expected a numeric IPv4 or IPv6 address");
  }
  const std::unique_ptr<addrinfo, decltype(&::freeaddrinfo)> address(raw_address, ::freeaddrinfo);
  const Socket socket = ::socket(address->ai_family, address->ai_socktype, address->ai_protocol);
  if (socket == invalid_socket) {
    fail_socket("orbit: TCP socket");
  }
  connection_->socket = socket;
  set_nonblocking(socket);
  connect_socket(socket, *address, deadline);
}

TcpClient::~TcpClient() = default;
TcpClient::TcpClient(TcpClient&&) noexcept = default;
TcpClient& TcpClient::operator=(TcpClient&&) noexcept = default;

void TcpClient::send(const rpc::Frame& frame) {
  if (!is_open()) {
    throw std::logic_error("orbit: TCP client is closed");
  }
  const auto deadline = Clock::now() + options_.timeout;
  const auto wire = rpc::marshal(frame, options_.max_payload);
  try {
    write_all(connection_->socket, wire, deadline);
  } catch (...) {
    close();
    throw;
  }
}

rpc::Frame TcpClient::receive() {
  if (!is_open()) {
    throw std::logic_error("orbit: TCP client is closed");
  }
  const auto deadline = Clock::now() + options_.timeout;
  try {
    std::array<std::uint8_t, rpc::header_size> bytes{};
    read_all(connection_->socket, bytes, deadline);
    const auto header = rpc::detail::read_header(bytes.data(), options_.max_payload);
    rpc::Frame frame{header.id, header.sequence, rpc::Bytes(header.length)};
    read_all(connection_->socket, frame.payload, deadline);
    return frame;
  } catch (...) {
    close();
    throw;
  }
}

bool TcpClient::is_open() const noexcept { return static_cast<bool>(connection_); }
void TcpClient::close() noexcept { connection_.reset(); }

}  // namespace orbit
