# 架构与使用约定

## 生命周期与资源所有权

`orbit.Component` 定义 `Start(context.Context) error` 和 `Stop(context.Context) error`。使用 `service.NewApp(timeout, components...)` 组合组件：按传入顺序启动，失败时逆序回滚已经成功的组件，正常退出时也逆序关闭。失败的 `Start` 必须自行释放部分初始化资源；`Stop` 必须幂等。

App、监听器、队列、discovery.Store 和 Session 均为单次使用对象，停止后重新创建。监听器同步完成绑定后 `Start` 才返回，端口占用错误不会延迟到后台才发现。TCP/HTTP/WebSocket 与 discovery.Store 提供 `Done` 和 `Err`，App 自动监督这些组件；监听循环意外退出会终止应用并返回原因。自定义组件也可以实现同样的两个方法参与监督。

根 context 取消后，App 为清理创建独立、有总时限的 context，避免把已经取消的 context 用来关库和释放资源。组件及回调必须遵守取消和超时；Go 无法安全强杀不返回的用户函数。App 不会主动结束进程，是否退出由入口决定。

数据库连接和 etcd 客户端由调用方拥有。使用 `Close` 关闭它们，并将这类依赖放在网络组件之前初始化，网络组件退出后再释放。etcd 的 `Registration.Close(ctx)` 负责撤销自己的租约，不会关闭共享客户端。HTTP 客户端退出时调用 `CloseIdleConnections`。

HTTP 的 `Start` context 用于初始化；请求通过 `Stop` 优雅排空。TCP Session 随其父 context 取消立即关闭；WebSocket 的已升级连接在服务器 `Stop` 时显式关闭。应用应通过 App 驱动统一退出。

需要完成已接收请求时使用可选 `App.RunGraceful`：正常退出先 Quiesce、逆序 Drain，再取消组件 context 并 Stop。
它保留排空期间的组件 context，覆盖 Session 同步回调、Queued 回调和待发帧；启动失败/运行故障仍立即取消。
原有 Run 和 Close 行为保持不变，排空不代表对端已确认。详细约束见 [生产能力说明](production.md)。

## 网络收发与并发模型

每条 Session 有一个读循环和一个写循环，回调在该 Session 的读循环中串行执行。不同连接的回调并行执行。`OnOpen` 在第一条入站消息之前调用，`OnClose` 在读写循环退出之后调用一次；`OnClose` 收到的 context 已取消，适合清理，不适合继续发送。回调 panic 会记录结构化日志、关闭连接并回收读写协程。

`Send` 复制 payload 并尝试放入有界队列。队列满返回 `socket.ErrQueueFull`；连接关闭返回 `socket.ErrClosed`；过大的报文返回 `rpc.ErrTooLarge`。返回 nil 只表示已入队，不表示已写入网络或对端已确认。关闭连接会放弃尚未发出的消息。需要可靠交付时，在应用协议中定义 ACK、幂等键与重试策略。

发送队列不被关闭，关闭状态与发送入队在同一锁下判定，通过 context 和关闭 transport 唤醒阻塞读写。这避免了旧版 `len(channel)` 检查后阻塞、向已关闭队列发送及写协程泄漏问题。发送消息的字节切片在调用返回后可由调用方继续修改。

`socket.Options` 的默认值：最大 payload 65535 字节，发送队列 256，写超时 10 秒，读超时 0（不限制空闲时间）。示例开启 60 秒读超时。可选 Admission 在监听器之间共享总量/每来源配额，InboundRate 限制每连接入站速率；零值维持旧行为。身份认证和访问控制由应用配置，详见 [生产能力说明](production.md)。

TLS 使用构造函数传入的 `*tls.Config`，它会被 Clone；证书和根证书由部署方提供。可设置 `MinVersion: tls.VersionTLS13`。没有默认信任所有证书或跳过校验。普通 TCP/WS 示例仅绑定本机回环地址。

## 帧格式与编解码

所有整数按网络大端序编码。TCP 是连续帧流，WebSocket 的每条二进制消息必须包含且只包含一帧：

| 偏移 | 长度 | 字段 |
| --- | --- | --- |
| 0 | 2 | payload 字节数，不包含消息头 |
| 2 | 2 | 消息 ID |
| 4 | 4 | 消息序号 |
| 8 | 2 | flags，必须为 0 |
| 10 | 指定长度 | payload |

空 payload 合法，最大长度 65535。TCP 解码错误意味着连接已失去帧同步，应关闭连接，Session 会自动执行。WebSocket 额外拒绝文本消息、截断帧、尾随字节和超过限制的消息。旧 flags=1/2 的加密报文被明确拒绝。

`codec.Registry` 按 ID 和 Go 指针类型注册消息：

```go
var registry codec.Registry
err := registry.Register(1, &MyMessage{}, codec.JSON{})
// 检查 err；Protobuf 类型使用 codec.Protobuf{}。
frame, err := registry.Encode(&MyMessage{}, 42)
// 检查 err 后调用 session.Send(frame)。
message, err := registry.Decode(frame)
```

JSON 编解码直接使用标准库 `encoding/json/v2`，Go 1.27.2 无需手动启用实验。默认拒绝重复字段和非法 UTF-8，字段名按大小写精确匹配；HTTP DecodeJSON 另外拒绝未知字段。协议兼容性变化见 [Go 1.27 与 JSON v2 适配](go127.md)。

注册时允许 typed-nil 指针作为类型样本，编码时拒绝 nil 消息。ID 0 不用于注册（底层帧可以透明传输 ID 0）。重复 ID 或类型会返回错误，且不改变原有注册表。消息类型及编解码器在注册后保持不变；自定义 Codec 本身也必须支持并发调用。

`socket.Router.Register` 绑定消息 ID，`Router.Handle` 可直接作为 Session Handler。`socket.Chain` 按给定顺序包裹中间件，第一个位于最外层。未知 ID 会返回错误并关闭 Session，应用可自行定义其他策略。

## 串行事件队列与定时任务

`service.NewQueue(capacity, interval, update, logger)` 创建实例级队列，Start 后用 `Post(func())` 投递任务。Post 始终不阻塞，包括从队列回调内部调用；满时返回 `service.ErrFull`。因此业务必须处理入队失败，而不是无限制积压。

需要将多个连接的业务状态集中到同一协程时，网络 Handler 可以返回 `queue.Post(...)` 的错误。在队列内解码和处理消息，并在处理失败时显式关闭对应 Session。队列的定时 update 也在同一串行执行上下文运行；回调和 update 的 panic 被记录后隔离，队列继续处理其他任务。

Stop 停止接受新任务并排空已接受任务。不要在队列回调中调用自己的 Stop 等待自己退出；应通知外层取消应用。将队列作为网络监听器之前的组件，这样网络先停止、队列再排空。

`util.After` 与 `util.Every` 是阻塞型定时助手，调用方管理其 goroutine 和 panic 策略。回调串行，慢回调会合并 ticker 的 tick，不会补发全部过期 tick。没有全局时间状态，也没有业务回调类型。大量任务可使用 util.TimeWheel[K] 由 Queue 推进，轮询/暂停计时使用 PollTimer；持久化调度仍由应用实现。详见 [时间轮设计](timers.md)。

`Session.Heartbeat` 同样是阻塞函数，通过 factory 生成应用心跳帧；它随传入 context 或 Session 关闭退出。心跳消息 ID、ACK 和超时判定属于应用协议，框架不固定它们。

## 重连与服务发现

`tcp.Reconnect` 接收地址、socket options、可选 TLS、退避上下界和 serve 函数。serve 应调用 `Session.Run` 并响应取消。每次成功连接生成新 Session，旧发送队列不重放。连接失败时退避指数增长到上限；context 可立即打断等待和拨号。当前不包含随机抖动和多地址负载均衡，集群规模较大时由调用方增加调度策略。

etcd 用独立前缀保存 `Descriptor{ID, Name, Address, Metadata}`，不包含游戏区服类型或关注列表策略。`Registry.Register` 使用事务防止重复 ID 覆盖，持续消费 keepalive 响应，取消后撤销租约。租约丢失通过 `Registration.Done/Err` 暴露；应用应降低就绪状态，并按退避策略重新注册。Registry 还监听注册 key 所有权；key 被删除或被其他租约接管也会报告失效。可选 Registrar 组件按退避策略安全重新注册，Registration.Update 仅在仍持有租约时更新。

`Registry.Watch` 先读取完整快照，再从读取版本的下一版监听，避免查询与 watch 之间丢事件。发生压缩会重新拉取快照；其他 watch 错误返回调用方。每次回调获得独立的完整快照，可以安全保留或修改。业务在快照变化后决定连接哪些节点、更新路由或负载均衡。通用 Pool 可根据 Store 快照及调用方注入的工厂创建、替换和关闭连接器；旧跨服路由和区服筛选策略没有迁入。

推荐使用 `Registry.NewStore` 创建受监督的目录缓存：首次同步成功后才完成 Start；以事务 revision 原子发布快照，提供 Get/All/Snapshot、Status 和 Observer。断流后指数退避并添加抖动，压缩或非法数据触发全量重载；加载失败保留有效快照但取消就绪。默认每 30 秒重新验证目录，避免空闲 watch 长时间失联而未被发现。注册租约恢复由独立 Registrar 处理，读取缓存仍由 Store 管理。详见 [Store 使用说明](discovery-store.md)。

## 配置、日志与存储

配置只接受单个 YAML 文档，限制 1 MiB，拒绝未知字段和重复键，随后执行 Validate。泛型 `config.Load[T]` 可装载应用自己的配置类型。时长使用 `10s`、`1m` 等明确单位。框架不读取全局命令行参数，不自动展开任意环境变量，也不打印完整配置和 DSN。

日志使用 `github.com/NumberMan1/log v0.3.1`，组件借用 Logger，示例显式创建独立 core 并输出 JSON 到 stdout。应用先停止组件，再 Close 日志以排空异步输出；库不关闭调用方日志，也不调用 Fatal。nil Logger 在构造时取得默认 core 的快照，使用前须配置输出器。原本地 RotatingWriter 已移除，文件轮转交给上游 FilePrinter；保留期、磁盘配额和日志收集由部署层管理。详见 [日志接入](../logging/README.md)。

MySQL 配置分别设置 MaxOpen 和 MaxIdle，初始化时 PingContext，失败时关闭池。业务调用 `QueryContext` / `ExecContext` / `BeginTx`，并自行处理 Rows.Close、Rows.Err 及事务提交；GORM 使用 `WithContext`。没有旧版反射 SQL 包装器和隐式迁表。

Redis 通过 context 化客户端提供单机/集群能力；集群不接受非零数据库编号。当前采用 v8 API，与仓库既有依赖系列保持一致，未宣称已升级到 Redis v9 API。框架不会全局共享 Redis 客户端。真正的服务器版本兼容性应在目标环境做集成验证。
