# util

提供不依赖业务模型的定时、集合、文本匹配和完整写入工具。WheelOptions.Logger 接收 `NumberMan1/log` 的 Logger，仅借用，见 [日志接入](../logging/README.md)。

- `TimeWheel[K]` 按 key 管理任务，单层槽位加圈数，由调用方 Advance；截止时间向上取整，支持取消/重设，周期任务合并错过的次数。
- 时间轮所有操作须在同一执行上下文中进行，不自行启动 goroutine；maxTicks 只限制推进槽数，不限制单槽回调数量或耗时。
- PollTimer 适合主动 update 检查，支持暂停/恢复。After/Every 是受 context 控制的阻塞函数，调用方负责执行 goroutine 和 panic 策略。
- Bitmap 使用 [0,size) 下标，重复增删不改变计数；Bytes/Load 复制数据。Bitmap 与 PollTimer 不内置并发同步。
- Matcher 是可并发读取的不可变 Unicode trie，词表由调用方注入；支持重叠匹配，返回 rune 半开区间，替换保留未匹配文本。
- WriteFull 补齐短写并拒绝零进展、非法计数；时间和 byte/string 转换优先使用安全标准库接口。

参见 [时间轮取舍与语义](../docs/timers.md)、[可运行示例](../examples/timers/README.md) 与 [其他工具边界](../docs/framework-extensions.md)。
