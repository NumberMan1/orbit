#include <orbit/tcp_client.hpp>

#include <stdexcept>

#include "rpc_header.hpp"
#include "tcp_transport.hpp"

namespace orbit {

struct TcpClient::Connection {
  detail::SocketRuntime runtime;
  detail::SocketHandle socket;
};

TcpClient::TcpClient(const std::string& ip, std::uint16_t port, TcpOptions options)
    : options_(options) {
  rpc::detail::validate_limit(options.max_payload);
  detail::validate_timeout(options.timeout);
  if (port == 0) {
    throw std::invalid_argument("orbit: expected a numeric IP and a nonzero port");
  }
  const auto deadline = detail::Clock::now() + options.timeout;
  connection_ = std::make_unique<Connection>();
  connection_->socket = detail::connect_tcp(ip, port, deadline, options.no_delay);
}

TcpClient::~TcpClient() = default;
TcpClient::TcpClient(TcpClient&&) noexcept = default;
TcpClient& TcpClient::operator=(TcpClient&&) noexcept = default;

void TcpClient::send(const rpc::Frame& frame) {
  if (!is_open()) {
    throw std::logic_error("orbit: TCP client is closed");
  }
  const auto deadline = detail::Clock::now() + options_.timeout;
  const auto wire = rpc::marshal(frame, options_.max_payload);
  try {
    detail::write_all(connection_->socket.get(), wire, deadline);
  } catch (...) {
    close();
    throw;
  }
}

rpc::Frame TcpClient::receive() {
  if (!is_open()) {
    throw std::logic_error("orbit: TCP client is closed");
  }
  const auto deadline = detail::Clock::now() + options_.timeout;
  try {
    return detail::read_frame(connection_->socket.get(), options_.max_payload, deadline);
  } catch (...) {
    close();
    throw;
  }
}

bool TcpClient::is_open() const noexcept { return static_cast<bool>(connection_); }
void TcpClient::close() noexcept { connection_.reset(); }

}  // namespace orbit
