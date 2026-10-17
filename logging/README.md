# 日志接入

Orbit 直接使用 `github.com/NumberMan1/log v0.3.1` 的 Logger/field 接口。本目录仅保留接入说明，不再提供本地日志实现或轮转 writer。

- Session、Queue、Store 和 TimeWheel 接收 `serverlog.Logger`，仅借用，不关闭。应用拥有底层 `*serverlog.L`，应先停止组件及回调，再 Close 日志 core。
- 未传 Logger 时，在构造时取得 `serverlog.DefaultLogger()` 的快照；上游默认 core 没有 Printer，会静默丢弃输出。应在构造组件前 Configure，或显式注入独立实例。后续替换默认 core 不会更新已有组件引用。
- `field.Error` 保留原始错误，`With` 添加公共字段；字段中的嵌套对象按只读使用。框架使用 Error 记录故障，不调用会退出进程的 Fatal。
- FilePrinter 支持异步缓冲、按小时/天轮转；默认队列满时阻塞，可选 DropNewest，并通过 OnError、Err、Dropped 观测故障和丢弃。
- 日志 Close 排空已接收任务，但没有 context 超时。先停生产者；不得在当前 Printer/错误回调中关闭所属 core 或向其递归写日志。
- 原 `NewRotatingWriter` / FileOptions / ErrRecordTooLarge 已移除。上游时间轮转不提供原实现的 MaxBytes/Backups 限额；保留期与磁盘配额由应用或日志收集部署管理。

典型装配（位于可返回 error 的应用入口中）：

```go
raw, err := serverlog.NewConfiguredLogger(serverlog.Config{
    Level: "info", Stdout: true, StdoutTyp: "json",
    // LogFilePath: "./logs", // 可选的异步文件输出
})
if err != nil {
    return err
}
defer raw.Close() // App.Run 返回后，最后排空日志；避免直接 os.Exit 跳过 defer。
logger := serverlog.NewServerLogger(raw).With(field.String("app", "worker"))
queue, err := service.NewQueue(128, 0, nil, logger)
if err != nil {
    return err
}
app, err := service.NewApp(5*time.Second, queue)
if err != nil {
    return err
}
return app.Run(ctx)
```

导入别名为 `serverlog "github.com/NumberMan1/log"`，字段包为 `github.com/NumberMan1/log/field`。高级文件策略用 NewFilePrinter 配置后 AddPrinter；core 接管其关闭责任。Sentry 仅在应用显式配置时启用。

可运行的完整入口见 [echo](../examples/echo/main.go)、[discovery](../examples/discovery/main.go) 和 [timers](../examples/timers/main.go)。本项目自身协议继续使用 JSON v2；日志序列化遵循所固定上游版本的实现。
