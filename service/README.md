# service

`App.RunGraceful` 可选启用 Quiesce → Drain → 取消运行 context → Stop 的分阶段关闭；Run 保留原有语义。
Queue.Status 提供队列压力、执行、拒绝与 panic 计数。详细依赖顺序和超时边界见 [生产能力说明](../docs/production.md)。

组合组件生命周期，并为需要单线程逻辑的应用提供串行执行队列。Queue 注入 `NumberMan1/log` 的 Logger，应用在队列停止后再关闭日志 core，见 [日志接入](../logging/README.md)。

- App 按传入顺序 Start，失败回滚已启动组件，退出时逆序 Stop；组件应自行清理失败 Start 的部分资源。
- 支持 Done/Err 的组件会被监督；意外结束触发应用退出。整个关闭过程共享一个超时预算，组件必须响应 context。
- Queue 的 Post 不阻塞，满队列返回 ErrFull；回调与周期 update 在同一 goroutine 串行执行，panic 被记录并隔离。
- Queue 需显式 Stop，Start 的 context 只用于启动检查。Stop 拒绝新任务并排空已接收任务；回调必须及时返回，不能同步等待自身 Stop。
- Queue 提供执行环境；大量定时任务使用 [util.TimeWheel](../util/README.md)，由 update 推进。
- `Nodes[T]` 是并发安全的成员索引，拒绝重复 ID；仅复制成员表，不负责成员的启动、关闭或内部状态同步。

参见 [生命周期约定](../docs/architecture.md) 与 [队列和时间轮示例](../examples/timers/README.md)。
