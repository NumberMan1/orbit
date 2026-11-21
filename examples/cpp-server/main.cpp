#include <orbit/request_server.hpp>

#include <iostream>

int main() {
  try {
    orbit::RequestServer server("127.0.0.1", 9003, {
        {1, [](std::stop_token, const orbit::rpc::Frame& frame) {
           return orbit::RequestReply{2, frame.payload};
         }},
    });
    std::cout << "Listening on 127.0.0.1:" << server.port() << ". Press Enter to stop.\n";
    std::cin.get();
    server.stop();
    if (auto error = server.error()) {
      std::rethrow_exception(error);
    }
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
