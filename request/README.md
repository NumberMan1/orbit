# request

在 Orbit 帧和 Session 之上提供有界请求响应匹配，保留 `rpc` 原有 10 字节消息头。

```go
client, err := request.New(session, request.Options{
    ResponseIDs: []uint16{2},
    MaxPending: 256,
    Timeout: 5 * time.Second,
})
// 检查 err。由应用负责启动、等待 Session.Run(client.Handler(pushHandler))。
defer client.Close()
reply, err := client.Call(ctx, rpc.Frame{ID: 1, Payload: payload}, 2)
```

- 一个 Session 只创建一个 Client；重连建立新 Client。Client 借用 Session，Close 只关闭调用层。
- Handler 必须运行在独立的读循环；不要在同一读回调里阻塞等待 Call。
- ResponseIDs 是协议声明，不能与请求或推送 ID 重叠。未知/迟到/重复响应被消费，不交给 pushHandler。
- MaxPending/Timeout 零值默认 256/5s；请求序号不重复使用，耗尽后返回 ErrSequenceExhausted。
- context 取消、默认超时、发送失败、断连和关闭都会释放等待项。错误响应的业务格式由应用定义。
- 不自动重试、不在重连后重放；成功表示匹配响应到达，不保证业务事务已提交。
- Call 从开始到返回参与 Session 排空；排空后新 Call 返回 socket.ErrDraining，已发出的调用仍接收响应。Client 自动登记 ResponseIDs，Server 自动登记取消 ID；这些 ID 必须与业务请求严格分离。
- `Status` 公开待处理量和累计事件计数；取消和响应同时发生时，响应到达计数不等于 Call 成功返回次数。

完整组合见 [网关示例](../examples/gateway/README.md) 和 [生产能力说明](../docs/production.md)。

Go `Client` 可以使用默认的明文 TCP 请求格式调用 C++23 `orbit::RequestServer`。双方约定请求与响应 ID，服务端保留原序号并拒绝同一连接的重复在途序号，载荷由应用编解码。C++ 服务端默认在接入连接上启用 `TCP_NODELAY`。见 [C++ 请求服务端示例](../examples/cpp-server/README.md)。

## 可选远端取消与元数据

- Client.Options.CancelID 和 ServerOptions.CancelID 必须由双方约定、保持一致且与请求/响应/推送 ID 不重叠。零值禁用；取消帧携带原 Sequence、空 Payload。仅取消本连接的匹配调用，未知或迟到取消忽略。
- Client 超时/取消后尝试发送一次控制帧；发送队列满时不重试，Status.CancelFailures 可观测。取消不会回滚已发生的业务副作用，也不能强制结束用户函数。
- 双方显式启用 Metadata 后，请求 Payload 采用 version=1 的 JSON 包装：deadline_unix_milli、trace_id、原始 payload（base64）。帧头与响应体不变，包装后仍受 Session.MaxPayload 限制。旧端点默认完全不启用，不能单方面打开。
- 截止时间使用 UTC Unix 毫秒，部署需同步时钟；服务端还施加自己的 Timeout 上限。trace_id 是 32 位小写十六进制关联标识，不是认证信息或完整 W3C span。可使用 observe.WithTraceID 传入或由客户端生成，网关转发保留同一关联标识。
- `NewServer(options, handler)` 只接管声明的 RequestIDs，异步执行并发安全的 handler，让读循环能继续接收取消；未知 ID 转交 next。MaxConcurrent/Timeout 默认 256/5s。序号必须在连接生命周期内唯一。
- 服务端并发名额到 handler 实际返回才释放，取消不提前释放；Session.Drain 跟踪这些异步请求。Handler 必须遵守 context。OnError 可回安全错误响应并保留连接；未配置时保留错误关闭策略。无跨节点去重或 exactly-once 承诺。
- handler 使用 Queued（包括 Shards.For）或嵌套 Async 时，服务端等待实际执行及其接受的子任务完成，期间保留 context、Pending 和并发名额；排队错误/执行错误统一进入 OnError。不得在 handler 返回后继续派生未跟踪的工作。

同一实体需要串行处理时，可将已启动的 Shards 接入 Server（先检查每一步 err）：

```go
queued, err := socket.Queued(workers.For(entityKey), handleRequest)
server, err := request.NewServer(request.ServerOptions{
    RequestIDs: []uint16{1}, CancelID: 9,
}, queued)
// 会话由外层启动：session.Run(server.Handler(otherMessages))。
// 停止时先排空会话，再停止 workers；排队返回不代表请求完成。
```

## 心跳检测

`NewHeartbeat(client, HeartbeatOptions{PingID, PongID, Interval, Timeout, MissLimit})` 复用会话的单个 Client，Run 在独立 goroutine 中随连接 context 运行；断线重建 Client 和 Heartbeat。
PongID 必须在 ResponseIDs 中；若启用了 Metadata，将 PingID 放入 RawRequestIDs。远端用 RespondHeartbeat 在业务解码前处理专用 Ping，保持序号、空载荷。
连续应答超时达到 MissLimit 返回 ErrUnresponsive；只有显式 CloseOnFailure 才关闭借用的 Session。调用方取消不触发失活关闭，本地等待表满等非超时错误直接返回，由应用处理。
本地进入排空后，心跳遇到 ErrDraining 正常返回 nil，不把有意停服判断为心跳失败；RespondHeartbeat 自动登记 Ping 控制 ID。
Status 提供健康、连续丢失次数、成功次数、最后应答时间和最近 RTT。RTT 包含本地排队和对端处理，不是纯网络时延；周期从上一轮探测完成后计算。心跳成功也不代替应用就绪检查。
