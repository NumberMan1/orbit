#include <orbit/rpc.hpp>

#include <iostream>
#include <stdexcept>

namespace {

using namespace orbit::rpc;

void require(bool condition, const char* message) {
  if (!condition) {
    throw std::logic_error(message);
  }
}

template <typename Exception, typename Operation>
void rejects(Operation operation, const char* message) {
  try {
    operation();
  } catch (const Exception&) {
    return;
  }
  throw std::logic_error(message);
}

void check_wire() {
  const Frame frame{7, 0x01020304, {'o', 'k'}};
  const Bytes expected{0, 2, 0, 7, 1, 2, 3, 4, 0, 0, 'o', 'k'};
  require(marshal(frame) == expected, "frame differs from the Go wire format");
  const auto decoded = unmarshal(expected, 2);
  require(decoded.id == frame.id && decoded.sequence == frame.sequence && decoded.payload == frame.payload,
          "decoded frame differs from input");

  const std::span<const std::uint8_t> wire(expected);
  for (std::size_t size = 0; size < wire.size(); ++size) {
    rejects<std::runtime_error>([&] { unmarshal(wire.first(size)); }, "accepted truncated frame");
  }
  auto extra = expected;
  extra.push_back(0);
  rejects<std::runtime_error>([&] { unmarshal(extra); }, "accepted trailing byte");
  auto flags = expected;
  flags[9] = 2;
  rejects<std::runtime_error>([&] { unmarshal(flags); }, "accepted nonzero flags");
}

void check_limits() {
  const Frame empty{0xffff, 0xffffffff, {}};
  const auto decoded = unmarshal(marshal(empty, 1), 1);
  require(decoded.id == empty.id && decoded.sequence == empty.sequence && decoded.payload.empty(),
          "empty payload or maximum ID/sequence changed");

  const Frame full{1, 2, Bytes(max_payload, 0xff)};
  const auto wire = marshal(full);
  require(wire.size() == header_size + max_payload && unmarshal(wire).payload == full.payload,
          "maximum-size payload changed");
  rejects<std::length_error>([&] { unmarshal(wire, max_payload - 1); }, "decoded an oversized payload");
  rejects<std::length_error>([&] { marshal(full, max_payload - 1); }, "encoded an oversized payload");
  for (const auto limit : {std::size_t{0}, max_payload + 1}) {
    rejects<std::invalid_argument>([&] { marshal(empty, limit); }, "accepted an invalid encoding limit");
    rejects<std::invalid_argument>([&] { unmarshal(wire, limit); }, "accepted an invalid decoding limit");
  }
}

}  // namespace

int main() {
  try {
    check_wire();
    check_limits();
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
