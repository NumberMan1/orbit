# C++ 请求服务端示例

在 Orbit 仓库根目录构建并运行示例。

```sh
cmake -S cpp -B bin/orbit-cpp -DCMAKE_BUILD_TYPE=Release
cmake --build bin/orbit-cpp --config Release
./bin/orbit-cpp/orbit_cpp_request_server
```

使用 Visual Studio 生成器时，运行 `bin/orbit-cpp/Release/orbit_cpp_request_server.exe`。服务端监听 `127.0.0.1:9003`，接收 ID 为 1 的请求，返回 ID 为 2、序号和载荷原样保留的响应。接入连接默认启用 `TCP_NODELAY`。按 Enter 停止。

另开终端发送一帧。客户端应输出 `id=2 sequence=42 payload=hello orbit`。

```sh
go run ./examples/client -addr 127.0.0.1:9003
```

在 Go 应用中，可对目标连接调用 `tcp.Dial` 创建 Session。对同一 Session 创建 `request.Client`，声明 `ResponseIDs: []uint16{2}`，并在独立 goroutine 中运行 `session.Run(client.Handler(nil))`。用 `client.Call(ctx, rpc.Frame{ID: 1, Payload: payload}, 2)` 发送请求并按序号等待响应。Go 客户端会自动分配连接内唯一的请求序号。Go 客户端的生命周期见 [请求层文档](../../request/README.md)，可运行的组合见 [互通测试](../client/cpp_request_test.go)。
