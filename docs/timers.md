# 定时任务：标准定时器、轮询计时器与时间轮

Queue 只提供串行执行上下文；After/Every 只负责单项等待或周期执行。按 key 管理大量独立任务、批量推进、取消与重设使用 `util.TimeWheel[K]`，三种工具按需求共存。

当前时间轮来自发布依赖 `github.com/NumberMan1/numbox v0.4.1`（导入路径为
`github.com/NumberMan1/numbox/pkg/timewheel`）。Orbit 的 `util.TimeWheel[K]` 是类型别名，
`util.NewTimeWheel` 保留原有 `WheelOptions.Logger`，将上游 slog 诊断适配到应用的 NumberMan1/log Logger。
没有本地 replace 或调度算法副本；原有调用方式及日志 core 所有权不变。

`ErrTimerExists`、`ErrClockBackwards` 和新增的 `ErrInvalidKey` 与上游哨兵一致。
错误文本前缀变为 `timewheel:`，调用方应使用 `errors.Is` 判断错误。

## 为什么游戏服务器常采用时间轮

许多游戏服务器要管理大量心跳检查、冷却结束、效果到期和延后任务，并已有固定 tick 的串行逻辑循环。将相近截止时间分入槽位，可以共享一次时钟推进，便于批量处理以及按 key 删除。任务容许几十毫秒粒度时，这种组织方式很合适。这里的业务仅为用途举例，框架没有实现上述业务逻辑。

这不意味着所有游戏服都采用时间轮，也不意味着它一定比 Go 的标准定时器快。`time.Timer` 不会为每个等待项常驻一个 goroutine；Go runtime 管理定时器和唤醒。`time.AfterFunc` 的回调执行才涉及 goroutine。Orbit 的 `util.After/Every` 是阻塞函数，大量独立调用通常需要调用方创建许多 goroutine，这是旧替代方案的真实成本。

| 工具 | 适合 | 成本与限制 |
| --- | --- | --- |
| `time.Timer` / context deadline | 少量超时、网络 I/O、独立取消 | runtime 管理到期顺序；不必自行维护槽位 |
| `util.After/Every` | 少量受生命周期管理的后台周期任务 | 每个独立阻塞调用需要自己的执行上下文；不是批量任务索引 |
| `util.PollTimer` | 已有 update 循环中的少量对象计时，模拟时钟、暂停/恢复 | 需要主动 Poll；扫描很多不活跃计时器会增加每帧成本 |
| `util.TimeWheel[K]` | 大量可按 tick 取整、需要 key 取消/重设的任务 | 单个 owner 推进，无每任务 goroutine；精度和单槽突发需权衡 |

单纯将每个旧时间轮任务变成一个 `go util.After(...)`，对本项目的迁移目标不合适。保留标准定时器作为 I/O 超时工具，同时补回独立的时间轮，边界更明确。

## 当前时间轮的语义

实现为单层槽位加圈数，不是分层时间轮：

- 构造时显式传入起始时间、正 Tick 和至少两个 Slots。key 为泛型可比较类型，没有角色 UID、保存类型或固定业务回调字段。
- key 必须等于自身；包含 NaN 的值或持有不可比较值的接口 key 被拒绝。Schedule/Reset 返回 ErrInvalidKey，Cancel 返回 false；nil 接口 key 可以使用。
- `Schedule(key, deadline, interval, callback)` 使用绝对截止时间。interval=0 为单次，正值为周期；重复 key 返回错误，过去或等于当前 tick 的截止时间在下一 tick 处理。
- 截止时间向上取整到 tick，绝不提前触发。同一槽中符合条件的任务按插入顺序处理。实际延迟还包含队列拥塞、回调耗时和追赶预算；不是硬实时调度器。
- `Cancel` 根据 map 和链表节点直接移除；`Reset` 保留回调与周期、更换截止时间。即使任务已进入当前待执行批次，也能被前面的回调取消或重设。
- 周期采用固定延迟：从本次 `Advance` 传入的 now 计算下一次时间。长暂停后合并错过的周期，不逐次补发；回调可取消自身。需要固定频率或逐次补发时，由应用明确设计相应策略。
- `Advance(now, maxTicks)` 有界推进槽位，返回是否追到 now；未追到时下次继续。maxTicks 限制处理槽数，**不限制同一槽中的任务数或回调耗时**。大量同时到期的任务仍可能造成单帧突发，需要分散截止时间、拆分 owner 或对业务执行另设预算。
- 空轮 Advance 直接更新到目标 tick；即使跨度超过 maxTicks，也可立即返回 caught=true。
- callback panic 被记录并隔离。递归 Advance 返回错误；回拨时钟返回 ErrClockBackwards。模拟时钟应始终向前推进，重启模拟则创建新轮。
- 所有方法由同一 owner 调用，不提供隐式锁或后台 goroutine。可以在回调中 Schedule/Cancel/Reset/Clear；跨 goroutine 操作必须投递到同一 Queue。

新增、取消、定位重设的索引操作通常为 O(1)；推进成本取决于经过的槽位及其任务数。很远的任务会在每轮被检查并减少圈数，不能宣称任意跨度下总开销都是 O(1)。如果存在海量跨度很大的任务，需要根据实际分布选择更大的轮覆盖范围，或评估分层时间轮/堆；本轮没有以未经验证的复杂实现替换单层轮。

## Queue 接入

完整可运行示例：[examples/timers/main.go](../examples/timers/main.go)。

```sh
go run ./examples/timers
```

示例每 10ms 由同一 Queue 推进时间轮，周期回调触发三次后取消自身并结束应用。update 使用 `time.Now()` 作为真实推进时间，以覆盖 ticker 丢失的通知；也可以传入应用自己的确定性模拟时钟。外部事件通过 `queue.Post` 修改 wheel，并处理队列满错误。不要另起 goroutine 直接调用 wheel.Cancel。

Wheel 不会替应用停止 Queue。关闭顺序为停止网络输入、排空 Queue、停止 update；之后可以安全 Clear 或释放 Wheel。回调内应通知外层取消应用，不要同步等待自身 Queue.Stop。

`PollTimer.Reset(now, delay, interval)` 则适用于更简单的主动检查：Poll 在首次到期时返回 true，单次计时器随后失活；周期计时器合并错过次数。Pause/Resume 保存剩余时长，暂停期间不推进。全部使用 `time.Time` / `time.Duration`，不再混用毫秒整数和纳秒 duration。

## 修复与验证

旧时间轮没有检查 slotNum；nil key 的任务未从槽移除；删除节点后继续通过被删除节点取 Next 可能漏删；回调修改链表会干扰遍历；槽位与圈数计算在整圈边界存在延迟风险。旧 PollTimer 的暂停剩余时长计算及时间单位混用也不可靠。新实现针对这些边界、回调内取消/重设、panic 隔离、追赶合并、时钟回拨和 10,000 个待处理任务的取消提供测试。

可运行局部基准：

```sh
go test ./util -run='^$' -bench='Benchmark(Wheel|Runtime)ScheduleCancel' -benchmem
```

切换 numbox 前的历史基准（Apple M4、Go 1.27.2）：预先保留 10,000 个远期任务，逐项取消并重建，时间轮约 85ns/op、128B/op、2 allocs/op；runtime AfterFunc 约 98ns/op、112B/op、1 alloc/op。这不代表当前 numbox 版本的性能。基准只测索引维护，不测到期执行、调度延迟、并发、真实游戏负载或整体内存占用；性能取舍仍需在当前版本及应用负载下验证。
