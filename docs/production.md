# 生产服务基础能力

本轮补充连接准入、过载保护、分阶段停服、健康检查、运行指标、请求响应匹配、节点选择和网关示例。
这些机制不提供账户认证、业务事务、持久化消息交付或跨节点状态迁移。

## 接入与过载保护

```go
admission, err := socket.NewAdmission(10000, 100)
// 检查 err；所有需要共享配额的监听器使用同一个 admission。
metrics := &socket.Metrics{}
options := socket.Options{
    Admission: admission,
    InboundRate: socket.RateLimit{PerSecond: 100, Burst: 200},
    Metrics: metrics,
    SendQueue: 256,
    ReadTimeout: 60 * time.Second,
}
```

- Admission 在 TCP 创建 Session、WS Upgrade 之前预留名额，包括尚未完成的握手。关闭、升级失败或配置失败都释放名额。
- 总量和每来源 host 的限制各自以 0 表示无限制；nil Admission 保留旧行为。来源使用直接对端地址，不信任转发头。经代理接入时，代理后连接可能共享同一来源配额，应在部署层选择适当策略。
- InboundRate 为每个 Session 的令牌桶，在解码/投递 Queue 前检查；零值禁用，速率必须有限且为正、Burst 至少为 1。超限返回 ErrRateLimited 并关闭连接。
- `LimitConcurrent(max, handler)` 非阻塞拒绝超出并发上限的处理。与 Queued 组合时使用 `Queued(queue, limitedHandler)`，让并发配额覆盖实际处理时间。
- 有界发送队列及 Queue 满时仍返回错误；不隐式等待、不悄悄丢弃。应用可在 Handler 内转换为自己的过载响应，若错误直接返回 Session 则关闭连接。
- 这些是进程内资源保护，不是分布式限流或完整防攻击方案。身份验证与访问策略由应用通过 OnOpen、Handler 或 Middleware 实现。

## 分阶段停服

原有 `App.Run` / `Session.Close` 保持立即取消语义。需要完成已接收请求时，使用 `App.RunGraceful(ctx)`：

1. 所有已启动组件逆序执行 `Quiesce()`，停止接入并将健康组件标记为不就绪。
2. 实现 `Drain(ctx)` 的组件按逆依赖顺序排空；此时依赖组件 context 保持可用。
3. 取消组件运行 context，逆序调用全部组件的 Stop。

整个关闭过程共享 NewApp 的一个总超时；启动失败或启动期间取消、受监督组件故障仍走立即取消及清理。
自定义 Quiesce 必须立即返回，Drain/Stop 必须遵守 context；Go 无法强制结束不返回的用户回调。

TCP Quiesce 关闭监听并封闭会话集合；WS 拒绝新 Upgrade，HTTP 拒绝新请求。
Session.Drain 拒绝新入站回调，等待已开始的同步回调、OnOpen、通过 Queued 接受的回调以及待发帧。
发送队列排空后关闭连接，超时则强制关闭。调用方不能在自身回调里同步等待 Drain；后台发送任务须由应用停止，
自行创建的 goroutine 不属于自动跟踪的请求。已断开且没有未完成工作的会话视为已排空。

排空仅意味着帧写入传输层，不等于对端确认或业务成功。半断连接、写错误、主动断线会终止请求；需要可靠交付时另行定义 ACK、幂等与持久化策略。
网络组件的依赖应在它之前加入 App，例如 `backend → queue → health → listener`，保证网关排空时后端连接和队列仍然可用。

## 健康检查与指标

`health.Registry` 是可选 Component，Start 后存活，Quiesce 后仍存活但不就绪，Stop 后两者均为 false。
通过 Add 注册具名检查，使用私有 mux 挂载 Liveness 和 Readiness，未自动安装任何全局端点。
Readiness 使用一个总超时、依次执行检查并捕获 panic，只公开检查名称和通过状态，不输出 DSN 或原始错误。
Check 必须支持并发调用并遵守 context；不通过遗留后台 goroutine 来伪造超时。

观测入口不绑定特定导出系统：

| 接口 | 内容 |
| --- | --- |
| `socket.Metrics.Snapshot()` | 活跃 Session、收发帧/字节、队列/速率拒绝、处理错误及累计耗时 |
| `Session.Status()` | 队列长度、尚未完成的发送、处理中回调、排空/关闭状态 |
| `Admission.Status()` | 连接占用和拒绝数 |
| `Queue.Status()` | 排队量、容量、接入状态、回调/更新执行量、panic、队列拒绝及累计耗时 |
| `Connector.Status()` | 当前连接状态、尝试次数、最后错误 |
| `Store.Status()` / `Pool.Status()` | 目录新鲜度、管理节点数量和协调错误 |
| `request.Client.Status()` | 待响应数量、匹配响应、取消/超时、迟到响应及关闭状态 |

Metrics 可由多个监听器共享，快照中的多个原子计数并非事务快照。HandlerNanos 包含分发和 Queued 回调执行，
不包含队列等待，不能直接当作端到端延迟或分位数。队列执行统计包含 update 回调；连接拒绝在 Admission 中计数。
框架不添加 user ID 等高基数字段；导出器、直方图和跨服务追踪上下文由应用显式接入。

## 请求响应与路由

`request.Client` 绑定一个 Session，一个 Session 仅使用一个 Client，Run 使用其 Handler。
ResponseIDs 必须与请求/推送 ID 分开；Call 指定期望的响应 ID，分配非零且不重复使用的 Sequence。
默认最多 256 个未完成调用、5 秒超时，调用方更短的 deadline 同样有效。到达 uint32 序号上限时要求建立新连接。
断线/Close/超时清理等待表，迟到或重复响应被消费而不会触发业务 Handler。Close 不关闭借用的 Session。
不得在该 Session 自己的同步读 Handler 中等待 Call，否则读循环无法处理响应。重连必须新建 Client，不重放请求。

Pool 负责维护组件；Picker 从实现 SessionProvider 且报告已连接的组件中选择，并在 Store 不就绪时拒绝新选择。
空 affinity 使用按稳定 ID 排序的轮询，非空 affinity 使用 rendezvous hash。返回的描述符为副本。
选择与网络发送之间仍可能断线；黏性 key 只决定路由，不保证实体所有权、数据迁移或 exactly-once。

`gateway.Forward` 组合应用 Route 与 Caller，限制并发转发，将后端序号映射回原客户端序号。
每个客户端仍遵循串行 Handler 模型，后端读循环独立运行。调用错误默认沿用 Session 的关闭策略；
错误码、鉴权、服务类型和业务绑定由应用定义，框架不自动重试写操作。

## 协议与可运行示例

`orbitgen` 从 JSON manifest 生成消息注册和 Handler 绑定；它复用已有 Go 类型，不替代 protoc。
使用方式见 [生成工具](../cmd/orbitgen/README.md)。

[网关示例](../examples/gateway/README.md) 启动两个后端、Store/Pool、网关及私有健康/指标接口。
静态目录仅用于无外部依赖演示，生产可替换为 etcd Store；演示不含账户认证，默认只监听回环地址。

```sh
go generate ./examples/gateway/protocol
go run ./examples/gateway -listen 127.0.0.1:0 -admin 127.0.0.1:0 -self-test
```

测试发送四次请求，验证两个后端均被选中、响应内容与序号正确、三个管理端点可用，然后排空退出。
