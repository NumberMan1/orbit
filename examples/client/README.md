# client 示例

向 echo 服务发送一条 TCP 帧，展示 Session 的运行、接收和关闭。

先启动 [echo](../echo/README.md)，再在仓库根目录运行：

```sh
go run ./examples/client -addr 127.0.0.1:9000
```

- Dial 后单独运行 Session.Run，发送 ID=1、Sequence=42、内容为 `hello orbit` 的帧。
- 整个操作受 5 秒 context 限制；收到回复后打印 `id=1 sequence=42 payload=hello orbit`。
- 退出时 Close 并等待 Done，避免遗留收发任务。这里只演示一次请求；持续连接使用 [Connector](../../socket/README.md)。
