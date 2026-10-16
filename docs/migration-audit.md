# rocommon 逐文件迁移核对

核对日期：2026-10-16。来源为用户指定的 `RO_Server_Trunk-branch_0.1.39/rocommon`。本表覆盖该目录全部 47 个 Go 源文件；逐项区分已迁移、标准库/架构替代及明确排除，避免将“有一个类似组件”误写成完整覆盖。

不承诺旧 API 原样兼容。替代项需修改调用方；未迁入业务协议、密钥、账号/支付/玩家状态及区服策略。源工程保持只读。

| 源文件 | 状态 | Orbit 对应能力 | 边界/修复 |
| --- | --- | --- | --- |
| `codec.go` | 已迁移通用部分 | codec.Registry.ByID/ByName/ByMessage、JSON v2/Protobuf、HTTP form | ConfirmMsgId 在原框架未使用，不宣称实现自动 RPC 请求匹配 |
| `def.go` | 已迁移通用部分 | socket.Session、MessageEvent/Reply、DecodeHandler/Queued、BeforeSend | 具体业务消息/握手字段不迁入 |
| `node.go` | 已重构 | orbit.Component、socket.NetworkOptions/Session.Values/Group、service.Nodes | 不保留动态属性强转；IDGenerator 代替业务位布局 |
| `rpc/aesctrcrypt.go` | 明确替代 | TLS/HTTPS/WSS | 不迁入重复 IV 与业务握手加密 |
| `rpc/msg.go` | 已迁移通用部分 | rpc.Frame/Read/Write、codec.Registry | 明文帧及序号透传；业务握手 ID 不迁入 |
| `rpc/rsacrypt.go` | 明确替代 | TLS/HTTPS/WSS | 不迁入内嵌密钥与业务握手 |
| `service/etcddesc.go` | 已迁移通用部分 | discovery.Descriptor | 标签放 Metadata；区服命名/业务筛选由调用方决定 |
| `service/etcdreg.go` | 已补齐 | Registry/Registration.Update、Registrar、Store、Pool | 独占 ID、key/租约所有权、自动恢复和节点协调 |
| `service/eventqueue.go` | 已重构 | service.Queue、Queued、TimeWheel/PollTimer | Init/Stop 用 Component；多个 update 可在组合回调内顺序调用 |
| `service/flag.go` | 标准库替代 | flag、示例 main | 库不解析应用命令行 |
| `service/init.go` | 明确替代 | service.App、示例 main、显式依赖 | 不恢复全局库/节点/SDK、开服时间副作用 |
| `service/multinode.go` | 已补齐 | service.Nodes[T]、discovery.Pool | 通用索引和动态生命周期，不携带区服规则 |
| `service/redisconnector.go` | 已迁移 | storage/redis.Open、Redis v8 原生接口 | context、TLS、单机/集群、显式 Close |
| `service/yamlconfig.go` | 已迁移通用部分 | config.Load[T]/Decode、应用配置类型 | 排除账号、SDK、支付等业务字段 |
| `socket/http/acceptor.go` | 已迁移 | http.Server、私有 mux、Shutdown | 真实监听与有界关闭 |
| `socket/http/connector.go` | 已补齐 | http.NewClient/DoJSON/DoForm | 任意 HTTP 方法、context、限量响应、非 2xx 错误 |
| `socket/http/respond_msg.go` | 已补齐 | http.WriteJSON | 保留状态码，编码失败不提前提交响应 |
| `socket/http/session.go` | 明确替代 | net/http.Request/ResponseWriter、DecodeJSON/DecodeForm | 不让 HTTP 假装具有 TCP 会话/握手接口 |
| `socket/mysql/connector.go` | 已重构 | storage/mysql.Open/ORM、database/sql | 连接池/context/参数化 SQL；Rows/事务直接使用标准接口，断线重连交驱动 |
| `socket/mysql/ormconnector.go` | 已重构 | storage/mysql.Open/ORM、database/sql | 连接池/context/参数化 SQL；Rows/事务直接使用标准接口，断线重连交驱动 |
| `socket/mysql/sqlparam.go` | 已重构 | storage/mysql.Open/ORM、database/sql | 连接池/context/参数化 SQL；Rows/事务直接使用标准接口，断线重连交驱动 |
| `socket/mysql/wrapper.go` | 已重构 | storage/mysql.Open/ORM、database/sql | 连接池/context/参数化 SQL；Rows/事务直接使用标准接口，断线重连交驱动 |
| `socket/nodeproperty.go` | 已迁移通用部分 | Session.SetValue/Value/DeleteValue、显式配置 | 不保留固定 serverType/zone/index 或反射写指针 |
| `socket/nodereg.go` | 明确替代 | 各包 NewServer/NewConnector、service.App | 不恢复 init 全局工厂与按字符串属性装配 |
| `socket/procrpc.go` | 已迁移通用部分 | DecodeHandler/Queued/Reply、BeforeSend、Router/Chain | 保留真实编解码/收发钩子；远程过程调用占位不算完整 RPC |
| `socket/runtimetag.go` | 已重构 | Component 生命周期、context、Done/Err | 消除旧运行标记和 WaitGroup 竞态 |
| `socket/sessionmanager.go` | 已补齐 | Group.Get/Snapshot/Len、Server.Session/SessionCount、IDGenerator | 拒绝重复 ID；默认 ID 只在进程内唯一 |
| `socket/socketoption.go` | 已补齐 | socket.Options.Network、超时/报文上限 | TCP/TLS/WS 缓冲区、NoDelay 和设置错误处理 |
| `socket/tcp/acceptor.go` | 已迁移 | socket/tcp.Server | 监听生命周期、会话管理与关闭 |
| `socket/tcp/connector.go` | 已补齐 | socket.Connector、tcp.NewConnector | 异步拨号、WaitSession、状态、退避、取消、新会话 |
| `socket/tcp/log.go` | 标准库替代 | slog、Options.Logger | 不恢复全局日志句柄 |
| `socket/tcp/roreuse_linux.go` | 标准库替代 | net.ListenConfig/HTTP Server | 不默认允许多个进程复用同一监听端口 |
| `socket/tcp/roreuse_windows.go` | 标准库替代 | net.ListenConfig/HTTP Server | 不默认允许多个进程复用同一监听端口 |
| `socket/tcp/session.go` | 已迁移通用部分 | 共享 socket.Session、对应 Transport | 有界发送、收发钩子、附加状态、地址；业务加密/心跳 ID 排除 |
| `socket/websocket/acceptor.go` | 已迁移 | socket/websocket.Server | 监听生命周期、会话管理与关闭 |
| `socket/websocket/connector.go` | 已补齐 | socket.Connector、websocket.NewConnector | 异步拨号、WaitSession、状态、退避、取消、新会话 |
| `socket/websocket/roreuse_linux.go` | 标准库替代 | net.ListenConfig/HTTP Server | 不默认允许多个进程复用同一监听端口 |
| `socket/websocket/roreuse_windows.go` | 标准库替代 | net.ListenConfig/HTTP Server | 不默认允许多个进程复用同一监听端口 |
| `socket/websocket/session.go` | 已迁移通用部分 | 共享 socket.Session、对应 Transport | 有界发送、收发钩子、附加状态、地址；业务加密/心跳 ID 排除 |
| `syncrecv.go` | 已补齐 | socket.Inbox、Connector.WaitSession | 有界接收、取消、关闭，不再只等待一次事件 |
| `sysmsg.go` | 已重构 | Options.OnOpen/OnClose、Connector.Status/WaitSession | 连接事件由显式钩子和状态表达 |
| `util/bitmap.go` | 已补齐 | util.Bitmap | 边界、幂等计数、复制所有权、装载校验 |
| `util/dfa_filter.go` | 已补齐 | util.Matcher | Unicode、重叠匹配、保留原文本；词表/归一化由应用注入 |
| `util/ioutil.go` | 已补齐/安全替代 | util.WriteFull、io/os、普通 byte/string 转换 | 拒绝零进展，不迁入 unsafe 可写字符串别名 |
| `util/loghelper.go` | 已迁移通用部分 | log/slog、logging.RotatingWriter | 级别/结构化日志/文件轮转；业务索引和日志上传不迁入 |
| `util/timer.go` | 已迁移通用部分 | util.PollTimer/After/Every、标准库 time | 暂停剩余时长、统一 duration；固定时区/5 点刷新属于业务 |
| `util/timewheel.go` | 已补齐 | util.TimeWheel[K] | 按 key 新增/取消/重设/周期、显式推进；无角色保存字段 |

原 go.mod/go.sum 不照搬，使用目标项目的 Go 1.27.2 和固定依赖。roserver/baseserver 不是此表的源码范围，不能因为名字带 base 就视为全部业务无关。

## 验证入口

- 时间轮/轮询/集合/IO：`util/*_test.go`；含边界、回调内删除/重设、暂停、重复计数、Unicode 重叠及局部基准。
- 消息/会话/连接器：`codec/registry_test.go`、`socket/extensions_test.go`、`socket/connector_transport_test.go` 和 TCP/TLS/WS 测试。
- HTTP/日志：`socket/http/form_test.go`、`logging/rotate_test.go`，验证限量、状态码、轮转与并发。
- 注册/目录/协调：`discovery/etcd_integration_test.go`、`pool_test.go`、`pool_integration_test.go`，验证删除恢复、接管防护、更新所有权、节点替换和失败清理，以及真实 etcd 发现到 TCP 收发/关闭的完整链路。

使用说明见 [补充能力接入](framework-extensions.md)；定时器设计见 [时间轮说明](timers.md)；完整运行记录见 [测试说明](testing.md)。
