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
- `Status` 公开待处理量和累计事件计数；取消和响应同时发生时，响应到达计数不等于 Call 成功返回次数。

完整组合见 [网关示例](../examples/gateway/README.md) 和 [生产能力说明](../docs/production.md)。
