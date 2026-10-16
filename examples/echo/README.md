# echo 示例

组合 TCP/WS 回显服务与 HTTP 健康检查，演示配置加载、依赖装配和信号退出。

在仓库根目录运行：

```sh
go run ./examples/echo -config examples/echo/config.yaml
```

- [配置](config.yaml) 默认监听 TCP 9000、HTTP 9001、WebSocket 9002，均绑定回环地址；空地址禁用对应服务，至少保留一个。
- TCP/WS 将收到的帧原样 Send，HTTP 提供 `GET /healthz`；回显不需要业务编解码注册表。
- 服务交给 App 按序启动、逆序停止；Ctrl+C 或 SIGTERM 触发有界关闭。
- 运行 [client](../client/README.md) 验证 TCP；此示例使用明文连接，不包含身份认证。
