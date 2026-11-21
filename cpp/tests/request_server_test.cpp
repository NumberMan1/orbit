#include "../src/tcp_transport.hpp"

#include <orbit/request_server.hpp>
#include <orbit/tcp_client.hpp>

#include <atomic>
#include <condition_variable>
#include <exception>
#include <future>
#include <iostream>
#include <mutex>
#include <stdexcept>
#include <system_error>
#include <thread>

#ifdef _WIN32
#include <ws2tcpip.h>
#else
#include <netinet/in.h>
#include <netinet/tcp.h>
#endif

#ifdef __linux__
#include <arpa/inet.h>
#include <cerrno>
#include <csignal>
#include <fcntl.h>
#include <filesystem>
#include <spawn.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>
#endif

namespace {

using namespace std::chrono_literals;

void require(bool condition, const char* message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

orbit::RequestReply echo(std::stop_token, const orbit::rpc::Frame& frame) {
  return {2, frame.payload};
}

void check_reply(orbit::TcpClient& client, std::uint32_t sequence) {
  client.send({1, sequence, {7}});
  const auto reply = client.receive();
  require(reply.id == 2 && reply.sequence == sequence && reply.payload == orbit::rpc::Bytes{7},
          "unexpected echo response");
}

bool tcp_no_delay(orbit::detail::Socket socket) {
  int value = 0;
#ifdef _WIN32
  int length = sizeof(value);
#else
  socklen_t length = sizeof(value);
#endif
  require(::getsockopt(socket, IPPROTO_TCP, TCP_NODELAY, reinterpret_cast<char*>(&value),
                       &length) == 0,
          "could not read TCP_NODELAY");
  return value != 0;
}

void check_no_delay() {
  using namespace orbit::detail;
  require(orbit::TcpOptions{}.no_delay, "TCP client must enable TCP_NODELAY by default");
  require(orbit::RequestServerOptions{}.no_delay,
          "request server must enable TCP_NODELAY by default");

  SocketRuntime runtime;
  const auto listener = listen_tcp("127.0.0.1", 0, 2);
  const auto port = bound_port(listener.get());
  for (const bool no_delay : {true, false}) {
    const auto client = connect_tcp("127.0.0.1", port, Clock::now() + 2s, no_delay);
    const auto accepted = accept_tcp(listener.get(), 2s, no_delay);
    require(static_cast<bool>(accepted), "could not accept test connection");
    require(tcp_no_delay(client.get()) == no_delay, "unexpected TCP_NODELAY on client");
    require(tcp_no_delay(accepted.get()) == no_delay, "unexpected TCP_NODELAY on accepted socket");
  }
}

void check_no_delay_opt_out() {
  orbit::RequestServer server("127.0.0.1", 0, {{1, echo}}, {.no_delay = false});
  orbit::TcpClient client("127.0.0.1", server.port(), {.no_delay = false});
  check_reply(client, 1);
}

void check_idle_shutdown() {
  orbit::RequestServer server("127.0.0.1", 0, {{1, echo}});
  orbit::TcpClient client("127.0.0.1", server.port());
  check_reply(client, 1);
  // Let the server enter its next read with the full 60s deadline.
  std::this_thread::sleep_for(100ms);
  require(server.stop_for(2s), "shutdown did not wake an idle session");
  require(server.stop_for(0ms), "completed shutdown is not idempotent");
}

void check_blocked_handler_shutdown() {
  std::promise<void> started;
  auto entered = started.get_future();
  std::promise<void> release;
  auto released = release.get_future();
  orbit::RequestServer server("127.0.0.1", 0, {
      {1, [&](std::stop_token, const orbit::rpc::Frame& frame) {
         started.set_value();
         // Wait up to 5s so a failing assertion can still clean up the fixture.
         released.wait_for(5s);
         return orbit::RequestReply{2, frame.payload};
       }},
  });
  orbit::TcpClient client("127.0.0.1", server.port());
  client.send({1, 1, {}});
  require(entered.wait_for(2s) == std::future_status::ready, "handler did not start");
  const bool stopped_early = server.stop_for(30ms);
  release.set_value();
  require(!stopped_early, "bounded shutdown succeeded with a blocked handler");
  require(server.stop_for(2s), "shutdown did not finish after the handler returned");
}

void check_disconnect_during_admission() {
  orbit::RequestServerOptions options;
  options.max_concurrent = 3;
  options.admission_timeout = 5s;
  std::mutex mutex;
  std::condition_variable_any gate;
  int started = 0;
  std::promise<void> canceled;
  auto canceled_notice = canceled.get_future();
  std::atomic<bool> waiting_ran{false};
  orbit::RequestServer server("127.0.0.1", 0, {
      {1, [&](std::stop_token stop, const orbit::rpc::Frame& frame) {
         std::unique_lock lock(mutex);
         ++started;
         gate.notify_all();
         gate.wait(lock, stop, [] { return false; });
         if (frame.payload == orbit::rpc::Bytes{2}) {
           canceled.set_value();
         }
         return orbit::RequestReply{2, {}};
       }},
      {3, [&](std::stop_token, const orbit::rpc::Frame&) {
         waiting_ran = true;
         return orbit::RequestReply{4, {}};
       }},
      {5, echo},
  }, options);
  orbit::TcpClient busy("127.0.0.1", server.port());
  orbit::TcpClient departing("127.0.0.1", server.port());
  busy.send({1, 1, {1}});
  busy.send({1, 2, {1}});
  departing.send({1, 1, {2}});
  {
    std::unique_lock lock(mutex);
    require(gate.wait_for(lock, 2s, [&] { return started == 3; }), "workers did not fill");
  }
  departing.send({3, 2, {}});
  std::this_thread::sleep_for(150ms);
  departing.close();
  require(canceled_notice.wait_for(1s) == std::future_status::ready,
          "waiting for admission delayed disconnect cancellation");

  orbit::TcpClient fresh("127.0.0.1", server.port());
  fresh.send({5, 1, {7}});
  require(fresh.receive().payload == orbit::rpc::Bytes{7}, "admission slot was not released");
  require(!waiting_ran, "disconnected session's waiting request ran");
}

void check_admission_expires_without_more_input() {
  orbit::RequestServerOptions options;
  options.max_concurrent = 1;
  options.admission_timeout = 150ms;
  std::promise<void> started;
  auto active = started.get_future();
  std::mutex mutex;
  std::condition_variable_any gate;
  orbit::RequestServer server("127.0.0.1", 0, {
      {1, [&](std::stop_token stop, const orbit::rpc::Frame&) {
         started.set_value();
         std::unique_lock lock(mutex);
         gate.wait(lock, stop, [] { return false; });
         return orbit::RequestReply{2, {}};
       }},
      {3, echo},
  }, options);
  orbit::TcpClient busy("127.0.0.1", server.port());
  busy.send({1, 1, {}});
  require(active.wait_for(2s) == std::future_status::ready, "worker did not start");
  orbit::TcpOptions client_options;
  client_options.timeout = 1s;
  orbit::TcpClient waiting("127.0.0.1", server.port(), client_options);
  waiting.send({3, 1, {}});
  bool closed = false;
  try {
    waiting.receive();
  } catch (const std::system_error& error) {
    closed = error.code() != std::errc::timed_out;
  } catch (const std::runtime_error&) {
    closed = true;
  }
  require(closed, "admission did not close a silent connection after timeout");
}

void check_duplicate_sequence() {
  std::mutex mutex;
  std::condition_variable_any gate;
  std::atomic<int> count{0};
  std::promise<void> started;
  auto active = started.get_future();
  std::promise<void> canceled;
  auto canceled_notice = canceled.get_future();
  orbit::RequestServer server("127.0.0.1", 0, {
      {1, [&](std::stop_token stop, const orbit::rpc::Frame&) {
         const bool first = count.fetch_add(1) == 0;
         if (first) {
           started.set_value();
         }
         std::unique_lock lock(mutex);
         gate.wait(lock, stop, [] { return false; });
         if (first) {
           canceled.set_value();
         }
         return orbit::RequestReply{2, {}};
       }},
  });
  orbit::TcpClient client("127.0.0.1", server.port());
  client.send({1, 41, {1}});
  require(active.wait_for(2s) == std::future_status::ready, "first request did not start");
  client.send({1, 41, {2}});
  require(canceled_notice.wait_for(1s) == std::future_status::ready,
          "duplicate sequence did not close the connection");
  require(count == 1, "duplicate sequence ran a second handler");
}

#ifdef __linux__
std::size_t open_socket_count() {
  std::size_t count = 0;
  for (const auto& fd : std::filesystem::directory_iterator("/proc/self/fd")) {
    std::error_code error;
    const auto target = std::filesystem::read_symlink(fd.path(), error);
    if (!error && target.native().starts_with("socket:[")) {
      ++count;
    }
  }
  return count;
}

void check_socket_released_while_handler_finishes() {
  std::promise<void> started;
  auto active = started.get_future();
  std::promise<void> canceled;
  auto canceled_notice = canceled.get_future();
  std::promise<void> release;
  auto released = release.get_future();
  orbit::RequestServer server("127.0.0.1", 0, {
      {1, [&](std::stop_token stop, const orbit::rpc::Frame&) {
         std::stop_callback notify(stop, [&] { canceled.set_value(); });
         started.set_value();
         released.wait_for(5s);
         return orbit::RequestReply{2, {}};
       }},
  });
  const auto baseline = open_socket_count();
  orbit::TcpClient client("127.0.0.1", server.port());
  client.send({1, 1, {}});
  require(active.wait_for(2s) == std::future_status::ready, "handler did not start");
  client.close();
  require(canceled_notice.wait_for(1s) == std::future_status::ready, "disconnect was not canceled");
  auto sockets = open_socket_count();
  for (auto deadline = std::chrono::steady_clock::now() + 1s;
       sockets > baseline && std::chrono::steady_clock::now() < deadline;) {
    std::this_thread::sleep_for(10ms);
    sockets = open_socket_count();
  }
  release.set_value();
  require(sockets == baseline, "handler retained the disconnected socket");
}

void check_child_does_not_inherit_listener() {
  orbit::RequestServer server("127.0.0.1", 0, {{1, echo}});
  const auto port = server.port();
  char program[] = "/bin/sleep";
  char duration[] = "10";
  char* args[]{program, duration, nullptr};
  pid_t pid;
  require(posix_spawn(&pid, program, nullptr, nullptr, args, environ) == 0, "could not spawn child");
  struct Child {
    pid_t pid;
    ~Child() {
      ::kill(pid, SIGTERM);
      ::waitpid(pid, nullptr, 0);
    }
  } child{pid};
  server.stop();
  orbit::RequestServer replacement("127.0.0.1", port, {{1, echo}});
  orbit::TcpClient client("127.0.0.1", port);
  check_reply(client, 1);
}

void check_descriptor_pressure() {
  rlimit previous{};
  require(getrlimit(RLIMIT_NOFILE, &previous) == 0 && previous.rlim_cur > 32,
          "need more than 32 descriptors to run this fixture");
  auto constrained = previous;
  constrained.rlim_cur = 32;
  require(setrlimit(RLIMIT_NOFILE, &constrained) == 0, "could not set descriptor limit");
  struct Limit {
    rlimit previous;
    ~Limit() { setrlimit(RLIMIT_NOFILE, &previous); }
  } limit{previous};
  orbit::RequestServer server("127.0.0.1", 0, {{1, echo}});
  orbit::TcpClient established("127.0.0.1", server.port());
  check_reply(established, 1);

  const int probe = ::socket(AF_INET, SOCK_STREAM | SOCK_CLOEXEC, 0);
  require(probe >= 0, "could not create probe socket");
  std::vector<int> files;
  for (int fd = ::open("/dev/null", O_RDONLY | O_CLOEXEC); fd >= 0;
       fd = ::open("/dev/null", O_RDONLY | O_CLOEXEC)) {
    files.push_back(fd);
  }
  const bool exhausted = errno == EMFILE && !files.empty();
  sockaddr_in address{};
  address.sin_family = AF_INET;
  address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  address.sin_port = htons(server.port());
  const auto reached = ::connect(probe, reinterpret_cast<sockaddr*>(&address), sizeof(address));
  std::this_thread::sleep_for(200ms);
  for (int fd : files) {
    ::close(fd);
  }
  ::close(probe);

  require(exhausted && reached == 0, "did not exercise accept under descriptor pressure");
  require(server.error() == nullptr, "descriptor pressure stopped the listener");
  check_reply(established, 2);
  orbit::TcpClient fresh("127.0.0.1", server.port());
  check_reply(fresh, 1);
}
#endif

}  // namespace

int main() {
  try {
    check_no_delay();
    check_no_delay_opt_out();
    check_idle_shutdown();
    check_blocked_handler_shutdown();
    check_disconnect_during_admission();
    check_admission_expires_without_more_input();
    check_duplicate_sequence();
#ifdef __linux__
    check_socket_released_while_handler_finishes();
    check_child_does_not_inherit_listener();
    check_descriptor_pressure();
#endif
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
