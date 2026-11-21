#include <orbit/request_server.hpp>

#include <condition_variable>
#include <iostream>
#include <mutex>
#include <string_view>

int main(int argc, char* argv[]) {
  try {
    orbit::RequestServerOptions options;
    options.max_connections = 1;
    options.max_concurrent = 2;
    options.max_pending_per_connection = 2;
    const std::string_view mode = argc > 1 ? argv[1] : "";
    if (mode == "fairness") {
      options.max_connections = 2;
      options.max_concurrent = 1;
      options.max_pending_per_connection = 1;
      options.admission_timeout = std::chrono::seconds{3};
    }
    if (mode == "pressure" || mode == "byte-pressure") {
      options.max_concurrent = 8;
      options.max_pending_per_connection = 8;
      if (mode == "pressure") {
        options.send_queue = 1;
      } else {
        options.send_queue_bytes = orbit::rpc::max_payload + orbit::rpc::header_size;
      }
      options.write_timeout = std::chrono::seconds{30};
    }
    std::mutex output;
    const auto notify = [&](std::string_view event) {
      std::lock_guard lock(output);
      std::cout << event << std::endl;
    };
    std::mutex mutex;
    std::condition_variable_any gate;
    bool released = false;
    orbit::RequestServer server("127.0.0.1", 0, {
        {1, [](std::stop_token, const orbit::rpc::Frame& f) {
           return orbit::RequestReply{2, f.payload};
         }},
        {3, [&](std::stop_token stop, const orbit::rpc::Frame& f) {
           notify("waiting");
           std::unique_lock lock(mutex);
           gate.wait(lock, stop, [&] { return released; });
           return orbit::RequestReply{2, f.payload};
         }},
        {5, [&](std::stop_token, const orbit::rpc::Frame&) {
           {
             std::lock_guard lock(mutex);
             released = true;
           }
           gate.notify_all();
           return orbit::RequestReply{6, {}};
         }},
        {7, [&](std::stop_token stop, const orbit::rpc::Frame&) {
           notify("waiting");
           std::unique_lock lock(mutex);
           gate.wait(lock, stop, [] { return false; });
           notify("canceled");
           return orbit::RequestReply{8, {}};
         }},
        {9, [&](std::stop_token, const orbit::rpc::Frame&) {
           orbit::rpc::Bytes payload(orbit::rpc::max_payload, 0x5a);
           notify("reply ready");
           return orbit::RequestReply{10, std::move(payload)};
         }},
    }, options);
    notify(std::to_string(server.port()));
    std::cin.get();
    server.stop();
    server.stop();
    if (auto error = server.error()) {
      std::rethrow_exception(error);
    }
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
