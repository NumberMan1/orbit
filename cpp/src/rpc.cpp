#include "rpc_header.hpp"

#include <algorithm>
#include <stdexcept>

namespace orbit::rpc {
namespace {

std::uint32_t read_be(const std::uint8_t* data, std::size_t size) {
  std::uint32_t value = 0;
  for (std::size_t i = 0; i < size; ++i) {
    value = (value << 8) | data[i];
  }
  return value;
}

void write_be(std::uint8_t* data, std::size_t size, std::uint32_t value) {
  while (size > 0) {
    data[--size] = static_cast<std::uint8_t>(value);
    value >>= 8;
  }
}

}  // namespace

void detail::validate_limit(std::size_t limit) {
  if (limit == 0 || limit > max_payload) {
    throw std::invalid_argument("orbit: payload limit must be between 1 and 65535");
  }
}

detail::Header detail::read_header(const std::uint8_t* data, std::size_t limit) {
  const auto length = read_be(data, 2);
  if (length > limit) {
    throw std::length_error("orbit: payload exceeds limit");
  }
  if (read_be(data + 8, 2) != 0) {
    throw std::runtime_error("orbit: unsupported frame flags");
  }
  return {length, static_cast<std::uint16_t>(read_be(data + 2, 2)), read_be(data + 4, 4)};
}

Bytes marshal(const Frame& frame, std::size_t limit) {
  detail::validate_limit(limit);
  if (frame.payload.size() > limit) {
    throw std::length_error("orbit: payload exceeds limit");
  }
  Bytes wire(header_size + frame.payload.size());
  write_be(wire.data(), 2, static_cast<std::uint32_t>(frame.payload.size()));
  write_be(wire.data() + 2, 2, frame.id);
  write_be(wire.data() + 4, 4, frame.sequence);
  std::copy(frame.payload.begin(), frame.payload.end(), wire.begin() + header_size);
  return wire;
}

Frame unmarshal(std::span<const std::uint8_t> wire, std::size_t limit) {
  detail::validate_limit(limit);
  if (wire.size() < header_size) {
    throw std::runtime_error("orbit: truncated frame header");
  }
  const auto header = detail::read_header(wire.data(), limit);
  if (wire.size() != header_size + header.length) {
    throw std::runtime_error("orbit: frame length mismatch");
  }
  return {header.id, header.sequence, Bytes(wire.begin() + header_size, wire.end())};
}

}  // namespace orbit::rpc
