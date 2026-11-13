# service

`App.RunGraceful` 可选启用 Quiesce → Drain → 取消运行 context → Stop 的分阶段关闭；Run 保留原有语义。
Queue.Status 提供队列压力、执行、拒绝与 panic 计数。详细依赖顺序和超时边界见 [生产能力说明](../docs/production.md)。

组合组件生命周期，并为需要单线程逻辑的应用提供串行执行队列。Queue 注入 `NumberMan1/log` 的 Logger，应用在队列停止后再关闭日志 core，见 [日志接入](../logging/README.md)。

- App 按传入顺序 Start，失败回滚已启动组件，退出时逆序 Stop；组件应自行清理失败 Start 的部分资源。
- 调用 Run 或 RunGraceful 前若 context 已取消，App 返回取消原因，组件不会启动。
- 支持 Done/Err 的组件会被监督；意外结束触发应用退出。整个关闭过程共享一个超时预算，组件必须响应 context。
- Queue 的 Post 不阻塞，满队列返回 ErrFull；回调与周期 update 在同一 goroutine 串行执行，panic 被记录并隔离。
- Queue 需显式 Stop，Start 的 context 只用于启动检查。Stop 拒绝新任务并排空已接收任务；回调必须及时返回，不能同步等待自身 Stop。
- Queue 提供执行环境；大量定时任务使用 [numbox timewheel.TimeWheel](../docs/timers.md)，由 update 推进。
- `NewShards(count, capacityPerShard, logger)` 将实体字符串 key 固定映射到多个 Queue：同一 key 串行，同一分片内按成功 Post 的加锁顺序执行，不同分片可并行。不同 key 可能碰撞到同一队列。
- Shards.Post 非阻塞；总排队上限为分片数 × 每分片容量，外加每分片一个正在执行的任务。`For(key)` 可传给 socket.Queued。Stop/Drain 先统一拒绝新任务，再排空；不在回调里同步等待自身停止。
- Shards 与 Queue 一样需要显式 Stop；不提供实体迁移、远程 Actor 或动态扩缩分片。将它放在网络组件之前加入 App，以便排空期间仍能执行已接受请求。
- `Nodes[T]` 是并发安全的成员索引，拒绝重复 ID；仅复制成员表，不负责成员的启动、关闭或内部状态同步。

参见 [生命周期约定](../docs/architecture.md) 与 [队列和时间轮示例](../examples/timers/README.md)。
