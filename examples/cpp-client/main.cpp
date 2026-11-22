#include <orbit/tcp_client.hpp>

#include <charconv>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <string_view>

namespace {

std::uint16_t parse_port(std::string_view text) {
  unsigned int port = 0;
  const auto* end = text.data() + text.size();
  const auto [last, error] = std::from_chars(text.data(), end, port);
  if (error != std::errc{} || last != end || port == 0 ||
      port > std::numeric_limits<std::uint16_t>::max()) {
    throw std::invalid_argument("expected a TCP port between 1 and 65535");
  }
  return static_cast<std::uint16_t>(port);
}

}  // namespace

int main(int argc, char* argv[]) {
  if (argc != 1 && argc != 3) {
    std::cerr << "usage: orbit_cpp_echo [HOST PORT]\n";
    return 1;
  }
  try {
    orbit::TcpClient client(argc == 3 ? argv[1] : "127.0.0.1",
                            argc == 3 ? parse_port(argv[2]) : 9000);
    const std::string_view payload = "hello orbit";
    const orbit::rpc::Frame request{1, 42, {payload.begin(), payload.end()}};
    client.send(request);
    const auto reply = client.receive();
    if (reply.id != request.id || reply.sequence != request.sequence || reply.payload != request.payload) {
      throw std::runtime_error("unexpected echo response");
    }
    std::cout << "id=" << reply.id << " sequence=" << reply.sequence << " payload=" << payload << '\n';
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
