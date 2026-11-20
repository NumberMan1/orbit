#pragma once

#include <cstddef>
#include <cstdint>
#include <span>
#include <vector>

namespace orbit::rpc {

inline constexpr std::size_t header_size = 10;
inline constexpr std::size_t max_payload = 65535;

using Bytes = std::vector<std::uint8_t>;

struct Frame {
  std::uint16_t id = 0;
  std::uint32_t sequence = 0;
  Bytes payload;
};

// A limit outside 1..max_payload throws std::invalid_argument. A payload
// larger than the limit throws std::length_error.
Bytes marshal(const Frame& frame, std::size_t limit = max_payload);

// Decodes exactly one frame. A truncated frame, trailing bytes or nonzero
// wire flags throw std::runtime_error. Limits behave as in marshal.
Frame unmarshal(std::span<const std::uint8_t> wire, std::size_t limit = max_payload);

}  // namespace orbit::rpc
