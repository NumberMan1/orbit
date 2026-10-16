# socket/websocket

将 WebSocket/WSS 连接适配为共享 Session，沿用 TCP 的消息处理接口。

- 每个 WebSocket 二进制消息必须恰好包含一份 rpc.Frame；拒绝文本消息、超限数据和帧后多余字节。
- 默认使用 Gorilla 的 Origin 检查；自定义 checkOrigin 由应用提供，Origin 检查不能替代身份认证。
- Server 组合 HTTP 生命周期与会话组，Stop 会显式关闭并等待已升级连接；仅调用 HTTP Shutdown 无法完成这些连接的清理。
- Dial 返回由调用方运行和关闭的 Session；NewConnector 提供退避重连，每次使用新会话，不重放旧消息。
- TLS 与底层 TCP 选项显式注入；业务处理应通过 Session，避免绕过其串行读写约束。

参见 [会话设计](../README.md) 与 [echo 示例](../../examples/echo/README.md)。
