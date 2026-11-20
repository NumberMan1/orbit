#pragma once

#include <orbit/rpc.hpp>

namespace orbit::rpc::detail {

struct Header {
  std::size_t length;
  std::uint16_t id;
  std::uint32_t sequence;
};

void validate_limit(std::size_t limit);
Header read_header(const std::uint8_t* data, std::size_t limit);

}  // namespace orbit::rpc::detail
