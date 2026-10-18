# socket

`Async(limit, handler)` 提供可选有界异步处理：让读循环继续接收取消/控制消息，复制载荷、跟踪排空、隔离 panic；同会话处理顺序不再串行，业务必须允许并发。默认 Run 的串行行为不变。
`Options.Observer` 可接入 [observe](../observe/README.md)；Queued 额外报告排队等待时间，观测回调必须及时返回。

可选 Admission 提供共享连接配额，InboundRate 在解码前限制每连接入站消息，LimitConcurrent 限制处理并发。
Session.Drain 支持已接受回调和发送排空；Status/Metrics 暴露压力和运行计数。
原有 Close 保持立即关闭，默认不启用配额/速率限制，完整语义见 [生产能力说明](../docs/production.md)。

在不同传输之上统一会话、消息处理与重连，具体监听和拨号位于子包。Options.Logger 使用 `NumberMan1/log` 的 Logger，组件仅借用；配置与关闭见 [日志接入](../logging/README.md)。

- Session.Run 单次运行，管理串行读取和发送循环；Send 复制 payload，有界队列满时返回错误。发送成功表示入队，不代表对端确认；Close 会中止待发送数据。
- Handler 与 OnOpen/OnClose 按会话串行执行，不同会话可并发。BeforeSend 可被并发调用，自定义钩子及跨会话状态需自行同步。
- Group 管理连接索引并拒绝重复 ID；默认 ID 仅进程内唯一，可注入 IDGenerator。Session 附加状态的 map 有锁，其中对象仍归应用管理。
- Router 按帧 ID 分发，Chain 组合中间件；DecodeHandler/MessageEvent 连接 codec 与类型化回复，Reply 保留 Sequence。
- Queued 将处理投递到应用队列，异步处理失败会关闭会话；Inbox 提供有界、可取消的同步接收，不自动匹配请求与应答。
- Connector.Start 异步拨号，WaitSession 等待连接；断线后退避创建新 Session，不重放消息。Stop 取消拨号、连接与等待。

传输：[TCP/TLS](tcp/README.md)、[WebSocket/WSS](websocket/README.md)；独立请求模型：[HTTP](http/README.md)。详见 [接入说明](../docs/framework-extensions.md)。
