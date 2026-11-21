# C++ TCP 回显示例

在仓库根目录启动 Go 回显服务。服务配置见 [Go echo 示例](../echo/README.md)。

```sh
go run ./examples/echo -config examples/echo/config.yaml
```

另开终端构建并运行 C++23 客户端。

```sh
cmake -S cpp -B bin/orbit-cpp -DCMAKE_BUILD_TYPE=Release
cmake --build bin/orbit-cpp --config Release
./bin/orbit-cpp/orbit_cpp_echo 127.0.0.1 9000
```

使用 Visual Studio 生成器时，运行 `bin/orbit-cpp/Release/orbit_cpp_echo.exe`。默认地址为 `127.0.0.1:9000`。

客户端核对响应中的 ID、序号和载荷，输出 `id=1 sequence=42 payload=hello orbit`。客户端默认启用 `TCP_NODELAY`。库的接入方式和套接字选项见 [Orbit C++](../../cpp/README.md)。
