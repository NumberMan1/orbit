# 补充框架能力接入

这些 API 补充旧 rocommon 中的通用能力，继续使用 Go 1.27.2、JSON v2 和显式生命周期。没有恢复旧全局注册表、业务消息 ID、固定密钥、区服类型或业务词库。

## 会话、消息与队列

`Group.Get/Snapshot` 和 TCP/WS Server 的 `Session/SessionCount` 提供会话查询。Session 默认 ID 仅在本进程内唯一；需要跨进程 ID 时，注入并发安全的 `Options.IDGenerator`。Group 拒绝重复 ID，不覆盖原连接，也不会因为移除被拒绝的连接而删掉原记录。

`Session.SetValue/Value/DeleteValue` 管理附加状态，map 操作有锁，但其中的可变对象仍由应用同步。OnClose 仍能读取状态，Run 结束时清空。LocalAddr/RemoteAddr 提供地址信息；不暴露原始 transport 供业务绕过串行读写约束。

`codec.Registry.RegisterNamed` 可显式命名协议，ByID/ByName/ByMessage 查询返回独立的元数据值。重复 ID、类型或名称均拒绝且不污染其他索引；默认名称包含 Go 包路径。

```go
handler, err := socket.DecodeHandler(registry, func(ctx context.Context, event socket.MessageEvent) error {
    // Message 是业务自己的 JSON/Protobuf 类型，Sequence 与接收时间被保留。
    return event.Reply(responseFor(event.Message))
})
// 检查 err。需要串行逻辑时继续包装：
handler, err = socket.Queued(queue, handler)
// 检查 err，再把 handler 交给 TCP/WS Server。
```

Queued 复制原始帧并记录入队前接收时间。队列满错误立即返回网络循环；异步处理失败或 panic 关闭对应会话。`Options.BeforeSend` 提供发送转换钩子，入队前执行、保留所有权隔离、失败直接返回。多个 Send 调用可能并发进入钩子，钩子须并发安全，不能递归 Send。接收链仍使用 Router/Chain。

`socket.Inbox` 是同步接收桥：将 `inbox.Handle` 交给 Session.Run，使用 `Receive(ctx)` 等待 FrameEvent。容量满时返回背压错误，不无限阻塞网络读取；Close 唤醒等待者并丢弃未读数据。它不替代业务请求/应答协议，不会自动判断哪个包属于哪个请求。

## TCP/WS 连接器

TCP 和 WebSocket 都提供 NewConnector，返回实现 Component 的 `socket.Connector`。Start 异步开始拨号，`WaitSession(ctx)` 等待真实连接；Status 暴露 Connected、Attempts、LastError。新连接创建新 Session，断线不重放旧发送队列。

```go
connector, err := tcp.NewConnector(address, socket.Options{}, tlsConfig, handler,
    socket.ConnectorOptions{RetryMin: 100*time.Millisecond, RetryMax: 5*time.Second})
// 检查 err，加入 service.App；WS 使用 websocket.NewConnector。
```

拨号失败或连接中断后指数退避并添加抖动。连接至少保持 StableFor（默认 30 秒）才重置退避，避免“连上即断”造成快速重连风暴。Stop 取消拨号、会话和等待，Done/Err 用于应用监督。Connected 只表示传输建立，不代表应用认证完成；WaitSession 返回后也可能立即断线，Send 错误必须处理。

`Options.Network` 支持 ReadBuffer、WriteBuffer 和可选 NoDelay，能应用于 TLS 内层 TCP。零缓冲区和 nil NoDelay 保留 Go/系统默认值。配置不支持的连接或设置失败会返回错误，不再忽略系统调用结果。

## 注册、目录与动态节点

三项能力分开管理：

- `Registry.Register` 返回一次租约注册。监听 key 所有权和 keepalive，key 删除或被其他租约接管都会报告 ErrLeaseLost。`Registration.Update` 使用租约比较事务更新 Descriptor，ID 不可更改，不覆盖后继实例。
- `Registry.NewRegistrar` 创建受管理的注册组件。首次注册同步完成，失败返回；运行中失效会退避重新注册，每次都以“key 不存在”的事务抢占。已有其他实例时持续报告未就绪、重试，不删除对方的 key。当前 Registrar 的 Descriptor 在其生命周期内固定；需要动态更新可手动管理 Registration.Update。
- `Store` 维护目录快照。`Pool` 根据完整快照创建、替换、移除组件，并在组件异常结束后重新创建。工厂通常返回 TCP/WS Connector；Select 由应用定义，不固定区服/角色规则。

```go
pool, err := discovery.NewPool(store, func(d discovery.Descriptor) (orbit.Component, error) {
    return tcp.NewConnector(d.Address, socket.Options{}, tlsConfig, handler, socket.ConnectorOptions{})
}, discovery.PoolOptions{
    Select: func(d discovery.Descriptor) bool { return d.ID != ownID && d.Name == wantedName },
})
// 检查 err。已有固定监听地址时，典型顺序：
app, err := service.NewApp(5*time.Second, store, pool, server, registrar)
// 检查 err，然后 app.Run(ctx)。registrar 先停，server/pool 再停，最后 Store。
```

Pool 默认每秒读取一份快照；同步工厂与 Start/Stop 必须响应 context，操作须及时完成。Descriptor 变化时先停止旧实例，再创建新实例；停止失败会保留旧实例等待后续清理，避免同 ID 两个组件并行存活。初始创建和替换失败会通过 Status 报告并重试。Pool 的 Count 表示被管理的组件数，不能当作已连通节点数；真实连接状态由 Connector 决定。

Store 不就绪时，Pool 保留既有节点并报告降级，不依据坏数据删节点。是否允许向这些节点发送数据由应用的就绪/降级策略决定。Pool.Get 返回借用的组件，查询后也可能被下一次协调停止。此协调有 Interval 延迟，不声称目录变化会立刻建立网络连接。

只需要节点索引时可用 `service.Nodes[T]`，它拒绝重复 ID，返回复制的成员表；不承担成员资源的启动与停止。

## HTTP、日志和工具

`http.DecodeForm` 限制正文大小、检查 MIME、保留重复字段并拒绝坏转义，只读取 URL-encoded 正文，不偷偷混入 URL 查询参数。`DoForm` 发送 url.Values、限量读取可选 JSON 响应。Multipart 文件上传使用标准库 MultipartReader，并由应用设置文件数/文件大小与清理规则；未迁入不完整的反射表单转换。

`http.WriteJSON` 在提交响应头前完成编码，保留指定状态码并返回写入错误；不再强制改为 200。204/205/304 等不允许正文的状态用标准库直接写响应头。

日志统一使用 `github.com/NumberMan1/log v0.3.1`。组件参数接收 `serverlog.Logger`，结构化字段使用 field 包；应用创建并拥有 core，停止所有组件后 Close。文件输出交给上游 FilePrinter，可配置队列、刷新周期、时间轮转、BlockOnFull/DropNewest 和 OnError，通过 Err/Dropped 观测。原本地 NewRotatingWriter 已移除，按大小限额/备份数不再由框架提供。默认 core 没有输出器，nil Logger 使用前需要应用 Configure。完整装配与关闭限制见 [日志接入](../logging/README.md)。

`util.Bitmap` 使用 [0,size) 边界，重复 Add/Remove 不改变计数，Load 校验长度和 padding 后才提交，Bytes/Load 都复制数据。旧 NewBitMap(max) 的 max 是包含端点的，新接口传入的是容量，迁移时注意换算。Bitmap 是 owner 管理的结构，没有内置并发锁。

`util.Matcher` 从调用方词表构造不可变 Unicode trie，支持重叠匹配，返回 rune 半开区间。Replace 保留未匹配文本、空格和符号；不会再擅自删除空格或 `&`。空词/非法 UTF-8 词条忽略，非法 UTF-8 输入不匹配且替换保持原样。无隐式大小写折叠、规范化或业务词库。扫描最坏为 O(文本长度 × 最大词长)，不是 Aho–Corasick 线性多模式实现。

`util.WriteFull` 补齐通用完整写入，拒绝零进展和非法返回计数。字节/字符串使用安全的 `[]byte(s)` 和 `string(b)` 转换；时间使用 time.UnixMilli/Unix、ParseInLocation、Date/AddDate，不恢复 unsafe 别名或固定凌晨 5 点的业务日历规则。时间轮和可暂停计时器见 [单独说明](timers.md)。
