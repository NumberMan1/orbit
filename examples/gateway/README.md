# 两后端网关示例

启动两个独立 TCP 后端（随机回环端口）、服务目录 Store、连接池 Pool、网关，以及私有 HTTP 管理端点。
展示消息注册生成、连接/消息限流、节点选择、请求响应匹配、运行指标和分阶段退出。

```sh
go generate ./examples/gateway/protocol
go run ./examples/gateway
```

默认网关监听 `127.0.0.1:9100`，管理接口监听 `127.0.0.1:9103`：

- `/livez`：进程组件存活。
- `/readyz`：目录就绪且两个后端已建立调用连接。
- `/metrics`：JSON 计数快照；本例所有配置了 Metrics 的进程内会话共用一个 collector。

按 Ctrl+C 或发送 SIGTERM 使用 RunGraceful 退出。应用不包含认证，只供本机演示；不要直接将管理接口暴露到公网。

也可自动验证请求往返和退出流程，端口设为 0 避免占用固定端口：

```sh
go run ./examples/gateway -listen 127.0.0.1:0 -admin 127.0.0.1:0 -self-test
```

输出四个请求的后端名称和序号，两个后端轮询接收请求；随后验证健康和指标端点并正常排空退出。
调用链为：客户端 Call → 网关 Forward → Picker → 后端 Call → EchoReply → 原客户端序号。

静态目录只在本例中实现，不依赖真实 etcd；实际部署可换成 `Registry.NewStore`。
每个后端 Connector 在 OnOpen 创建一个 request.Client，在 OnClose 关闭；新连接不会复用旧等待表。
组件顺序保证网关排空时后端与连接池仍可用。后端服务地址、鉴权、故障切换、业务错误与黏性 key 按应用需要配置。
