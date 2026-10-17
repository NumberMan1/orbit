# 迁移记录

## 来源与范围

来源为本机 `RO_Server_Trunk-branch_0.1.39`，核心参考目录 `rocommon`（原模块声明 Go 1.12），并检查 `roserver/baseserver` 以界定业务边界。目标分支从本地 `master` 的 `20e659df17a1761552287586d2a39d0f4f92ef50` 创建，当前名称为 `feat/server-framework`。该 master 起点只有仓库说明、许可证及忽略规则，没有沿用 develop 的实现。

本次采用通用能力提取和实现重构，没有原样复制旧项目。接口存在破坏性变化，旧项目需要修改导入、启动流程和消息处理代码。源目录未被修改。

## 源能力与目标对照

| 源文件/目录（相对 rocommon） | 目标 | 决策 |
| --- | --- | --- |
| `def.go`、`node.go`、`sysmsg.go` | `orbit.Component`、`socket.MessageEvent/Inbox`、`Session`、收发钩子 | 显式依赖和错误，替换动态节点工厂与业务属性接口 |
| `codec.go` | `codec.Registry`、JSON/Protobuf、`socket/http` | 实例级按 ID/名称/类型查询、JSON v2/Protobuf；HTTP 使用限量 DecodeForm/DoForm 和 url.Values，移除反射表单转换 |
| `syncrecv.go` | socket.Inbox、Connector.WaitSession、context | 补齐有界同步接收、可取消连接等待 |
| `rpc/msg.go` | `rpc/frame.go`、`codec.Registry` | 保留明文帧结构，修复边界与错误传播 |
| `rpc/aesctrcrypt.go`、`rsacrypt.go` | TCP/TLS、HTTPS、WSS | 不迁入固定密钥、重复 IV 及业务握手 |
| `socket/procrpc.go` | `socket.Router/Chain`、`DecodeHandler/Queued`、`BeforeSend` | 迁移实际收发钩子及队列处理；仅移除未实现的远程调用占位和全局注册 |
| `socket/sessionmanager.go`、`runtimetag.go` | `socket.Group.Get/Snapshot`、Session.IDGenerator、取消/Done | 补查询、自定义 ID 与重复 ID 防护；默认 ID 只保证进程内唯一 |
| `socket/nodeproperty.go`、`nodereg.go`、`socketoption.go` | 显式构造函数、NetworkOptions、Session.Value、context | 不再依赖全局节点工厂和反射属性写入 |
| `socket/tcp` | `socket/tcp`、共享 Session | TCP/TLS、受管理 Connector、缓冲区/NoDelay、超时；使用跨平台标准库监听，不默认启用端口复用 |
| `socket/websocket` | `socket/websocket`、共享 Session | WS/WSS、受管理 Connector 自动重连、单帧检查、同源检查、已升级连接关闭 |
| `socket/http` | `socket/http` | HTTP/HTTPS、私有 mux、Shutdown、限量 JSON/表单和保留状态码的 WriteJSON |
| `service/init.go`、`flag.go`、`multinode.go` | `service.App`、`service.Nodes[T]`、示例入口 | 依赖顺序启动、回滚、故障监督、逆序关闭；不在库初始化时解析参数或启动连接 |
| `service/eventqueue.go` | `service.Queue` | 串行回调、显式背压、实例级状态、停止排空 |
| `service/etcddesc.go`、`etcdreg.go` | `discovery` | 中性描述、Store、Registrar 重注册、租约所有权更新、Pool 节点协调；筛选/路由规则由应用注入 |
| `service/redisconnector.go` | `storage/redis` | context 化客户端、TLS、连接检查与显式 Close |
| `socket/mysql` | `storage/mysql`、标准库 `database/sql` | 修复连接池上限、初始化失败泄漏，保留可选 GORM |
| `service/yamlconfig.go` | `config` | 严格 YAML 与校验，去掉业务和第三方 SDK 配置 |
| `util/loghelper.go` | `github.com/NumberMan1/log v0.3.1` | 结构化字段、可选异步时间轮转/上报；不内置业务日志索引 |
| `util/ioutil.go` | `util.WriteFull`、`io`、`os` | 补完整写入；拒绝零进展，使用安全 byte/string 转换 |
| `util/timer.go`、`timewheel.go` | `util.TimeWheel[K]`、`PollTimer`、`After/Every` | 时间轮改用 numbox 发布依赖，保留暂停/恢复；Queue 只是推进/执行上下文，不能单独替代时间轮 |
| `util/bitmap.go`、`dfa_filter.go` | `util.Bitmap`、`Matcher` | 补通用算法；修复重复计数、数据别名、Unicode/重叠匹配；词库由调用方传入 |
| `baseserver/set` 等 rocommon 外业务工具 | 范围外 | 不作为 rocommon 框架源码整体迁入 |

没有迁入：`roserver` / `roweb` 的业务服务器、`serverproto` 生成协议、游戏数据表、账号登录、玩家/角色、战斗、排行榜、公会、GM、支付与第三方 SDK、机器人、跨服业务路由、AOI、共享内存业务同步、业务 Elasticsearch 索引。虽然 `baseserver` 名为基础服务器，其实现直接依赖上述协议与模型，所以没有整体迁入。

## 重要修复

1. **报文长度与溢出**：原版将上限强转为 uint16，并在报文超限时记录后把错误清空。现在校验 1..65535 的上限，拒绝超限和不支持的 flags，任何截断都返回错误；编码错误不再强制断言造成 panic。
2. **WebSocket 越界**：原版对未检查长度的数据直接读取消息头。现在先限制消息大小，再共享帧解码，并检查尾随字节。
3. **队列和收发竞态**：移除 `len(channel)` 后发送、nil 哨兵和关闭数据队列的竞争方式。关闭状态与入队互斥，任一方向失败都会关闭 transport 并唤醒另一方向。
4. **心跳泄漏**：原版 select 中的 break 无法退出外层循环。现在心跳、重连、定时任务都有明确取消路径。
5. **启动/关闭错序**：原版后台设置运行状态、Stop 中临时 Add WaitGroup，可能导致漏停与错误计数。现在启动状态在同步路径建立，使用 Go 1.25 `WaitGroup.Go` 管理已归属任务，并限制对象重用。
6. **HTTP 生命周期**：替换空 Stop、忽略 Serve 错误、请求构建错误返回 nil、响应无上限等行为；使用优雅 Shutdown、必要时强制 Close。
7. **etcd 一致性**：持续读取 keepalive；注册时不覆盖同 ID 的另一个实例；从快照 revision+1 开始 watch；压缩后重新拉取；取消时撤销自己的租约。
8. **存储资源**：修正两次 SetMaxIdleConns 而遗漏 SetMaxOpenConns 的问题；失败 Ping 关闭池；去掉旧 Rows 包装器的错误路径泄漏风险。
9. **配置与可测试性**：移除全局初始化副作用、业务字段和完整配置日志；重复注册失败不再污染部分映射；回调 panic 有日志和资源回收。

## 接入迁移步骤

1. 将业务留在独立模块，引用 `github.com/NumberMan1/orbit`，由业务模块维护消息、模型、认证与路由策略。
2. 使用新版 `protoc-gen-go` 为业务协议重新生成代码，接入 `google.golang.org/protobuf`。旧 gogo/golang protobuf 生成物不能假定直接满足新接口。
3. 将全局 `RegisterMessageInfo` 改为实例 `codec.Registry.Register`，将旧回调链改为 `socket.Handler` / Router / Middleware。旧 SessionAccepted/Connected 逻辑转到 OnOpen，Closed 清理转到 OnClose。
4. 将 `Start() ServerNode` / `Stop()` 改为带 context 且返回 error 的生命周期。依赖先启动，监听器后启动。不要忽略 Send、Post 和存储调用的错误。
5. 旧消息头的 flags=0 可以复用；业务握手与 flags=1/2 必须协调客户端、网关和服务端一起迁移到 TLS。此版本不提供旧加密协议兼容开关。
6. 将 etcd ID、元数据和关注关系转换到 Descriptor 与应用自己的路由策略。可用 Registrar 自动监督 key/租约丢失并安全重注册，也可手动监督 Registration.Done。目录读取使用 discovery.Store，并根据 Status.Ready 决定是否允许使用缓存；Pool 结合注入的连接器工厂维护节点。
7. 将旧 YAML 转成新配置，独立注入数据库/证书凭据。不要直接拷贝旧配置目录、SDK 密钥、业务地址和业务协议。
8. 运行目标环境的集成测试与负载测试。此迁移没有宣称原游戏业务无修改即可启动，也没有给出未经测量的吞吐提升数据。

## 后续 Store 改进

借鉴 mc-rogue `infrastructure/component/config/gameserver.Store` 的机制，新增业务无关的服务目录 Store：窄 Backend/Watcher、事务版本原子快照、带抖动的退避恢复、压缩/坏数据重新加载、定期同步及就绪状态。设计中防止失败重载后丢失恢复状态、只收到 watch 创建响应便推进版本等问题。未复制其区服、分组、玩家模型或业务配置 CAS 写入接口。原 Registry.Register 的独占 ID 与租约所有权保持不变；低层 Watch 复用新的 etcd 适配层。使用及验证见 [Store 文档](discovery-store.md)。

## Go 1.27 与 JSON v2 升级

目标工具链由 Go 1.25.1 调整为 Go 1.27.2，go.mod、.go-version、Makefile 和 CI 一致固定该补丁版。框架 JSON 路径改为标准库 encoding/json/v2，使用 Go 1.27.2 默认配置，无需实验开关；HTTP 流式读取继续限制正文大小并拒绝尾随值。字段大小写、nil 集合、omitempty 和非法输入处理存在语义变化，详见 [适配说明](go127.md)。

## 第二轮逐项补齐（2026-10-16）

此前以简化组件代替旧能力的描述过宽，尤其遗漏了时间轮、可暂停计时器、位图/匹配、同步接收、发送钩子、会话查询/附加状态、WS 重连、状态码/表单、日志轮转、注册恢复和动态节点协调。本轮已补上这些通用能力，详见 [逐文件迁移核对](migration-audit.md)、[时间轮设计与取舍](timers.md) 和 [补充能力接入](framework-extensions.md)。迁移仍不承诺旧 API/旧业务协议直接兼容。

## 日志库切换（2026-10-16）

当时切换到远程 tag `github.com/NumberMan1/log v0.3.0`（提交 `d824d7d09c1666a56860ab483463520a2c186ca8`），go.mod/go.sum 固定版本与校验值。之前对本地旧代码的缺陷结论不能直接套用到此 tag；该版本已修复级别禁用幂等性、动态配置并发和 JSON 兜底，并提供文件故障报告与队列溢出策略。

这是日志 API 的破坏性变化：`*slog.Logger` 参数改为 `serverlog.Logger`，日志键值对改用 field，原 logging 包中的 RotatingWriter 被移除。应用拥有并关闭 core，组件只借用；默认 core 不含输出器，应在构造组件前配置或显式注入。时间轮转与原按大小轮转不同，需另行设置部署侧保留期与磁盘配额。详见 [日志接入](../logging/README.md)。

## numbox 时间轮与日志依赖更新（2026-10-17）

时间轮改用 GitHub 发布依赖 `github.com/NumberMan1/numbox v0.3.0`，对应提交
`f87ec986a57535725c8618bbc180699a80615af1`。`util.TimeWheel[K]` 是上游类型别名，
构造函数仅保留 NumberMan1/log 日志适配，现有调用无需更换 Logger 类型。
错误哨兵直接复用上游，文本前缀变为 `timewheel:`；新增非法 key 检查和空轮快速推进，详见 [时间轮说明](timers.md)。

日志升级到 `github.com/NumberMan1/log v0.3.1`，对应提交
`6674654a2833df929cefda48b32d7f28d6e37557`。保留应用拥有日志 core、组件仅借用的生命周期约定。
两个依赖均固定发布 tag 与 go.sum 校验值，不通过本地路径替换。
