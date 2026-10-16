# socket

在不同传输之上统一会话、消息处理与重连，具体监听和拨号位于子包。Options.Logger 使用 `NumberMan1/log` 的 Logger，组件仅借用；配置与关闭见 [日志接入](../logging/README.md)。

- Session.Run 单次运行，管理串行读取和发送循环；Send 复制 payload，有界队列满时返回错误。发送成功表示入队，不代表对端确认；Close 会中止待发送数据。
- Handler 与 OnOpen/OnClose 按会话串行执行，不同会话可并发。BeforeSend 可被并发调用，自定义钩子及跨会话状态需自行同步。
- Group 管理连接索引并拒绝重复 ID；默认 ID 仅进程内唯一，可注入 IDGenerator。Session 附加状态的 map 有锁，其中对象仍归应用管理。
- Router 按帧 ID 分发，Chain 组合中间件；DecodeHandler/MessageEvent 连接 codec 与类型化回复，Reply 保留 Sequence。
- Queued 将处理投递到应用队列，异步处理失败会关闭会话；Inbox 提供有界、可取消的同步接收，不自动匹配请求与应答。
- Connector.Start 异步拨号，WaitSession 等待连接；断线后退避创建新 Session，不重放消息。Stop 取消拨号、连接与等待。

传输：[TCP/TLS](tcp/README.md)、[WebSocket/WSS](websocket/README.md)；独立请求模型：[HTTP](http/README.md)。详见 [接入说明](../docs/framework-extensions.md)。
