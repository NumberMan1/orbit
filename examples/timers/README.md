# timers 示例

直接导入 `github.com/NumberMan1/numbox/pkg/timewheel`，通过 `logging.NewSlogLogger` 接入借用的 NumberMan1/log Logger。

展示 Queue 与时间轮的组合：Queue 提供串行执行环境，TimeWheel 管理按 key 索引的定时任务。

在仓库根目录运行，无需外部服务：

```sh
go run ./examples/timers
```

- 轮精度为 10ms、256 个槽；注册首延迟约 30ms、周期 100ms 的 pulse 任务。
- update 使用实际 time.Now 推进，每次最多追赶 64 个槽；所有运行中的时间轮操作在同一 Queue 上执行。
- 输出三次 pulse 后，回调取消自身并通知外层停止 App；不在回调中等待自身 Queue.Stop。另有 2 秒总 context 上限。
- 外部并发事件应通过 Queue.Post 操作时间轮，并处理队列满错误。

精度、追赶与集中到期限制见 [时间轮说明](../../docs/timers.md)。
