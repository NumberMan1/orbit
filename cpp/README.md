# Orbit C++

C++23 库提供 Orbit 的 RO 帧编解码、同步 TCP 客户端和 TCP 请求服务端。库使用 C++ 标准库和系统套接字，支持 Windows、Linux 和 macOS。需要支持 C++23 的编译器及 CMake 3.20 或以上版本。

标准库需提供 `std::jthread` 和 `std::stop_token`。使用 Apple 自带工具链时，可用 Xcode 26 或以上版本。 [Xcode 26 发行说明](https://developer.apple.com/documentation/xcode-release-notes/xcode-26-release-notes)

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
#include <orbit/tcp_endpoint.hpp>

int main() {
  const auto endpoint = orbit::parse_tcp_endpoint("localhost:9000");
  orbit::TcpClient client(endpoint.host, endpoint.port);
  client.send({1, 42, {'h', 'i'}});
  const auto reply = client.receive();
  const bool echoed = reply.id == 1 && reply.sequence == 42 &&
                      reply.payload == orbit::rpc::Bytes{'h', 'i'};
  return echoed ? 0 : 1;
}
```

## 接口约定

- `orbit::rpc::marshal` 和 `unmarshal` 编解码单帧。`unmarshal` 接受 `std::span<const std::uint8_t>`，要求缓冲区恰好包含一帧。应用可在自己的 TLS 或 WebSocket 传输上使用这两个接口。
- `orbit::parse_tcp_endpoint` 拆分 `host:port` 或 `[host]:port`。包含冒号的主机部分用方括号包住。函数保留主机部分的原始内容，端口默认要求非零，传入 `TcpEndpointUse::listen` 时可用 `0`。
- `orbit::TcpClient` 同步读写明文 TCP。出站地址交给系统解析器处理，可传入 IPv4、IPv6 或系统可解析的名称，例如 `"127.0.0.1", 9000`、`"::1", 9000` 或 `"localhost", 9000`。每次建立连接时按系统解析顺序尝试候选，剩余候选均分连接剩余时间。连接由客户端持有，`close()` 可重复调用，析构也会关闭；同一实例的操作需串行调用。
- `TcpOptions` 的 `max_payload` 接受 1–65535，默认 65535；空载荷合法。`timeout` 接受 1–2147483647 毫秒，默认 5 秒，分别限制一次连接、一次完整发送和一次完整接收。连接计时在解析前开始，平台的同步 DNS 调用无法中断。解析成功时若已到截止时间，客户端会报超时。接收超时涵盖帧头和载荷。
- `TcpOptions::no_delay` 默认开启 `TCP_NODELAY`，减少连续发送小帧时的延迟。设为 `false` 可启用 Nagle 算法。
- 参数无效时抛出 `std::invalid_argument`，载荷超限时抛出 `std::length_error`，主机名解析失败、协议错误与读帧过程中对端关闭时抛出 `std::runtime_error`，套接字错误和超时抛出 `std::system_error`。I/O 或入站帧解析失败后客户端会关闭连接；调用方可创建新的客户端。出站帧在写入前被拒绝时可以继续使用连接。
- ID 和序号原样传输，业务载荷和请求响应匹配由应用定义。帧格式见 [Go rpc 文档](../rpc/README.md)。

运行步骤见 [C++ 回显示例](../examples/cpp-client/README.md)。

## C++ 请求服务端

`orbit::RequestServer` 同步绑定数值 IPv4 或 IPv6 地址并开始监听。端口传入 `0` 时，`port()` 返回系统分配的端口。构造时注册消息 ID 及回调。回调返回应用约定的响应 ID 和原始字节，服务端使用请求序号回包。Go 请求方应声明该响应 ID。见 [请求服务端示例](../examples/cpp-server/README.md)。

- 各连接分别读取和串行写入帧，同一连接的回调可以并发完成，回复按完成情况发送。回调需要支持并发调用。载荷由应用解码，可以直接携带 Protobuf 二进制数据。
- `RequestServerOptions::no_delay` 默认在每条接入连接上开启 `TCP_NODELAY`。设为 `false` 可启用 Nagle 算法。套接字选项配置失败时关闭该连接。
- 默认最多 64 个连接、8 个全局在途请求。每连接的 `max_pending_per_connection` 默认取全局容量的一半并向上取整，包含该连接等待名额的请求，可以显式配置。全局饱和时每连接最多保留一帧，按等待顺序分配名额，默认等待期限为 1 秒。读循环继续检测断线；等待过期、等待期间再收到完整请求或达到单连接上限时关闭该连接。
- 每连接的发送队列默认最多 32 个待写回复、总计 256 KiB。达到任一上限即关闭该连接。字节上限至少要能容纳一个完整的最大帧，正在写入的帧不计入队列。连接数超限时关闭新连接。
- 默认每次读帧限时 60 秒，每次写帧限时 5 秒，包含完整帧头和载荷。未知请求 ID、零序号、重复在途序号、非法帧、读写超时、回调异常或无效回复会关闭该连接。序号从请求进入等待或执行队列起保留，直到回调完成且回复入队或会话关闭。业务错误可以由回调按应用协议编码进正常响应。
- 断线会取消会话的 `std::stop_token`，丢弃该连接的待处理请求和待发回复。读写线程退出后释放套接字，业务回调可继续用取消令牌做清理。服务端停止时会取消所有会话并等待回调返回。拥有者可在回调外调用 `stop()` 或 `stop_for(timeout)`，均可重复调用。`stop_for` 超时返回 `false` 时须保留服务端对象，等待业务回调结束，析构仍会等待。通过 `error()` 检查监听器运行期间的致命错误。
- 文件描述符或内核缓冲资源暂时不足时，监听器退避后继续接受新连接，已有连接继续工作。
- Linux 在获取套接字时原子设置 close-on-exec。macOS 在获取后用 `fcntl` 设置 `FD_CLOEXEC`，与同时启动子进程之间存在短暂的继承窗口。 [Apple `fcntl` 说明](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/fcntl.2.html)
- 传输使用明文 TCP，Go 端使用默认请求格式。第一版的互通范围是标准请求和响应帧。见 [Go 请求层](../request/README.md)。

传入 `-DORBIT_CPP_TEST_GO_INTEROP=ON` 重新配置可启用双向 Go/C++ 回归测试，需要本机 Go 1.27.2。见 [测试说明](../docs/testing.md#c-客户端与请求服务端)。
