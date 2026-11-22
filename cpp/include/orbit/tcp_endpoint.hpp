#pragma once

#include <charconv>
#include <cstdint>
#include <stdexcept>
#include <string>
#include <string_view>

namespace orbit {

struct TcpEndpoint {
  std::string host;
  std::uint16_t port;
};

enum class TcpEndpointUse { connect, listen };

// Splits host:port or [host]:port without interpreting the host or resolving it.
// A zero port is valid only for listeners. Invalid syntax throws invalid_argument.
inline TcpEndpoint parse_tcp_endpoint(std::string_view endpoint,
                                      TcpEndpointUse use = TcpEndpointUse::connect) {
  constexpr auto message = "orbit: expected host:port or [host]:port";
  const auto separator = endpoint.rfind(':');
  if (separator == std::string_view::npos) {
    throw std::invalid_argument(message);
  }
  auto host = endpoint.substr(0, separator);
  if (host.starts_with('[') && host.ends_with(']')) {
    host.remove_prefix(1);
    host.remove_suffix(1);
  } else if (host.contains(':')) {
    throw std::invalid_argument(message);
  }
  if (host.empty() || host.contains('\0') ||
      host.find_first_of("[]") != std::string_view::npos) {
    throw std::invalid_argument(message);
  }

  const auto service = endpoint.substr(separator + 1);
  const auto end = service.data() + service.size();
  std::uint16_t port = 0;
  const auto [next, error] = std::from_chars(service.data(), end, port);
  if (error != std::errc{} || next != end || (port == 0 && use != TcpEndpointUse::listen)) {
    throw std::invalid_argument("orbit: invalid TCP port");
  }
  return {std::string(host), port};
}

}  // namespace orbit
