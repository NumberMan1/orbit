#include <orbit/request_server.hpp>

// Keep a reference to the server implementation in the shared-library link test.
extern "C" std::uint16_t orbit_shared_server_port() {
  orbit::RequestServer server("127.0.0.1", 0, {
      {1, [](std::stop_token, const orbit::rpc::Frame&) { return orbit::RequestReply{2, {}}; }},
  });
  return server.port();
}
