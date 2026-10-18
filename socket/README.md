# socket

`Async(limit, handler)` 提供可选有界异步处理：让读循环继续接收取消/控制消息，复制载荷、跟踪排空、隔离 panic；同会话处理顺序不再串行，业务必须允许并发。默认 Run 的串行行为不变。
`Options.Observer` 可接入 [observe](../observe/README.md)；Queued 额外报告排队等待时间，观测回调必须及时返回。

可选 Admission 提供共享连接配额，InboundRate 在解码前限制每连接入站消息，LimitConcurrent 限制处理并发。
Session.Drain 支持已接受回调和发送排空；Status/Metrics 暴露压力和运行计数。
原有 Close 保持立即关闭，默认不启用配额/速率限制，完整语义见 [生产能力说明](../docs/production.md)。

Close 立即取消并关闭传输；Run 返回、OnClose 和会话状态清理等待已接受的 Queued/Async 回调退出。回调必须响应取消，不能同步等待自己的 Done/Stop。
Drain 期间继续接收已登记的控制/响应 ID，忽略新的业务帧；自定义协议使用 Options.DrainIDs 或 AllowDuringDrain 登记，不能把业务请求 ID 加入白名单。

在不同传输之上统一会话、消息处理与重连，具体监听和拨号位于子包。Options.Logger 使用 `NumberMan1/log` 的 Logger，组件仅借用；配置与关闭见 [日志接入](../logging/README.md)。

- Session.Run 单次运行，管理串行读取和发送循环；Send 复制 payload，有界队列满时返回错误。发送成功表示入队，不代表对端确认；Close 会中止待发送数据。
- Handler 与 OnOpen/OnClose 按会话串行执行，不同会话可并发。BeforeSend 可被并发调用，自定义钩子及跨会话状态需自行同步。
- Group 管理连接索引并拒绝重复 ID；默认 ID 仅进程内唯一，可注入 IDGenerator。Session 附加状态的 map 有锁，其中对象仍归应用管理。
- Router 按帧 ID 分发，Chain 组合中间件；DecodeHandler/MessageEvent 连接 codec 与类型化回复，Reply 保留 Sequence。
- Queued 将处理投递到应用队列，默认异步处理失败会关闭会话。Execute 在队列外等待 handler 及其接受的嵌套 Queued/Async 工作，将错误返回给请求拥有者；request.Server 已集成此行为。不要在目标队列内同步 Execute，也不要在回调返回后继续使用其执行 context 投递工作。Inbox 提供有界、可取消的同步接收，不自动匹配请求与应答。
- Connector.Start 异步拨号，WaitSession 等待连接；断线后退避创建新 Session，不重放消息。Stop 取消拨号、连接与等待。

传输：[TCP/TLS](tcp/README.md)、[WebSocket/WSS](websocket/README.md)；独立请求模型：[HTTP](http/README.md)。详见 [接入说明](../docs/framework-extensions.md)。
