# Orbit C++

C++23 客户端库通过 Orbit 的 RO 帧协议与 Go 服务通信。库使用 C++ 标准库和系统套接字，支持 Windows、Linux 和 macOS。需要支持 C++23 的编译器及 CMake 3.20 或以上版本。

## 构建与接入

在 Orbit 仓库根目录构建并测试。

```sh
cmake -S cpp -B bin/orbit-cpp -DCMAKE_BUILD_TYPE=Release
cmake --build bin/orbit-cpp --config Release
ctest --test-dir bin/orbit-cpp -C Release --output-on-failure
```

在自己的 CMake 工程中，把 Orbit 放在 `third_party/orbit`，然后链接 `orbit::cpp`。

```cmake
add_subdirectory(third_party/orbit/cpp)
target_link_libraries(my_client PRIVATE orbit::cpp)
```

```cpp
#include <orbit/tcp_client.hpp>

int main() {
  orbit::TcpClient client("127.0.0.1", 9000);
  client.send({1, 42, {'h', 'i'}});
  const auto reply = client.receive();
  const bool echoed = reply.id == 1 && reply.sequence == 42 &&
                      reply.payload == orbit::rpc::Bytes{'h', 'i'};
  return echoed ? 0 : 1;
}
```

## 接口约定

- `orbit::rpc::marshal` 和 `unmarshal` 编解码单帧。`unmarshal` 接受 `std::span<const std::uint8_t>`，要求缓冲区恰好包含一帧。应用可在自己的 TLS 或 WebSocket 传输上使用这两个接口。
- `orbit::TcpClient` 同步读写明文 TCP。传入数值 IPv4 或 IPv6 地址及非零端口（例如 `"::1", 9000`）。连接由客户端持有，`close()` 可重复调用，析构也会关闭；同一实例的操作需串行调用。
- `TcpOptions` 的 `max_payload` 接受 1–65535，默认 65535；空载荷合法。`timeout` 接受 1–2147483647 毫秒，默认 5 秒，分别限制一次连接、一次完整发送和一次完整接收。接收超时涵盖帧头和载荷。
- 参数无效时抛出 `std::invalid_argument`，载荷超限时抛出 `std::length_error`，协议错误与读帧过程中对端关闭时抛出 `std::runtime_error`，套接字错误和超时抛出 `std::system_error`。I/O 或入站帧解析失败后客户端会关闭连接；调用方可创建新的客户端。出站帧在写入前被拒绝时可以继续使用连接。
- ID 和序号原样传输，业务载荷和请求响应匹配由应用定义。帧格式见 [Go rpc 文档](../rpc/README.md)。

运行步骤见 [C++ 回显示例](../examples/cpp-client/README.md)。传入 `-DORBIT_CPP_TEST_GO_INTEROP=ON` 重新配置可启用 Go/C++ 回归测试，需要本机 Go 1.27.2。
