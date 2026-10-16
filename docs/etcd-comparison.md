# etcd 实现对比

评审日期：2026-10-15。先对比 Orbit 引入 Store 前的实现与本机 `mc-rogue/server/infrastructure/component/config/etcd`，并沿实际调用链查看 `config/gameserver`、`component/discovery` 及其锁定的 kvregistry 依赖。

本次已按后续要求借鉴 Store，增加业务无关的 `discovery.Store`。下表保留首次评审的基线，已落地变化见后文；不要将基线中的缺项视为当前实现状态。Orbit 随后已升级至 Go 1.27.2 与 JSON v2，对方代码未修改。

## 结论与范围

**本次采用 Orbit 的业务无关服务发现边界，并借鉴 mc-rogue 配套热配置 Store 的快照和恢复机制。** 两者用途不同，不能据此宣称整体更成熟或性能更高。

指定的 `config/etcd` 主要是启动配置加载器，不是服务注册中心。它通过 Loader 读取数据库、Redis、区服配置，并将热配置交给 `config/gameserver.Store`。Orbit 的 `discovery.Registry` 负责租约注册，新增 `discovery.Store` 负责服务目录缓存；当前仍没有等价的远程配置 Store；本地 YAML 加载也不能代替远程配置热更新。

| 维度 | Orbit 引入 Store 前 | mc-rogue 对应实现 | 基线判断 |
| --- | --- | --- | --- |
| 无业务依赖 | Descriptor、ID、Name、Address、Metadata；可自定义前缀 | Loader 直接依赖 DatabaseConf、GameServerConf、玩家公共 Redis；Discovery 固定业务服务类型与前缀 | Orbit 更符合本次迁移边界 |
| 启动配置易用性 | 没有远程配置专用 API | LoadJSON、LoadDatabaseByID、LoadCommonConfig、CLI 装配齐全 | mc-rogue 更方便 |
| 热配置恢复 | Watch 压缩后重新 List，其他错误返回监督方 | Store 自动退避、从 revision+1 恢复，压缩/非法批次重新加载，保留有效快照 | mc-rogue Store 更完整 |
| 配置写入一致性 | 没有通用配置 CAS/批量写接口 | ModRevision CAS、依赖版本比较、批量事务、按 revision 发布快照 | mc-rogue Store 更强；不能算成 Orbit 服务发现缺陷 |
| 服务注册所有权 | Version(key)==0 事务占有；重复 ID 返回错误；撤销自己的租约 | Discovery 调用 kvregistry.Register；该依赖复用已有租约或直接 Put，没有同等独占 ID 事务 | Orbit 的重复实例防护更明确 |
| 生命周期与取消 | 注册/监听/关闭显式接收 context，客户端由调用方持有 | Loader 的 LoadJSON/load 不接受 context；Store 有 Start/Close 生命周期；Manager.Close 无超时参数 | Orbit 核心接口更清晰；不能把 Loader 的不足套到整个 Store |
| 快照读取与观察 | 推送完整快照并复制 Metadata；无内置查询缓存、revision/滞后指标 | Store 原子发布快照、版本查询、Observer；Manager 提供查询、增删改回调和慢回调日志 | mc-rogue 功能更完整 |
| 测试接口 | 绑定具体 *clientv3.Client，依赖真实 etcd 测试关键行为 | Loader/Store/Manager 有窄后端接口和 fake；Store 包含恢复、冲突、批次、并发快照测试 | mc-rogue 更利于故障注入；数量不等于实际验证结果 |
| Go 版本 | 1.25.1 | infrastructure 同样为 1.25.1，Store 已使用 atomic.Pointer 等能力 | 不能仅凭新版语法判断优劣 |

## 指定 Loader 的具体不足

1. **构造函数的 error 约定不完整。** `loader.go:77` 的 NewLoader 直接调用 kvregistry.NewRegistry。锁定版本 `v1.0.28-0.20260713032525-f4f2d90099ea` 的 `etcd/etcd_registry.go` 在 configure/Ping 失败时执行 log.Panic。因此，返回签名虽带 error，连接初始化失败仍可能 panic。mc-rogue 的 Discovery 已有 safeNewKVRegistry 包装，但 Loader 没有复用它。
2. **调用方不能取消单次配置读取。** `loader.go:97`、`:108` 没有 context 参数；底层 Get 支持 context option，但该层只传 WithExactlyKey。底层仍有固定超时，不能误称为完全没有超时；问题是无法继承应用启动取消和更短的请求期限。
3. **组合配置不保证同一版本。** `loader.go:146` 先读区服，再单独读分组，两次读取没有绑定同一个 revision。并发更新时可能组合出不同时间点的配置。其底层普通 Get 还使用 WithSerializable，允许读到较旧状态。配套 Store 的版本化快照路径更适合需要一致组合的读取。
4. **业务边界不适合直接迁入。** `LoadGameServer`、`LoadPlayerPublicRedis`、DatabaseID 及 CLI/GORM 装配绑定现有项目。应提取通用读取和校验接口，业务 key、schema 与应用装配留在业务层。

## 已借鉴并落地的 Store 机制

- **窄后端接口**：Backend.List/Watch 与 Watcher.Next/Close 将缓存状态机和 etcd 传输分离，可以用 fake 注入故障；etcd 客户端仍由调用方管理。
- **原子版本快照**：同一 revision 的事务先完整校验，再一次性发布；Get/All/Snapshot 复制调用方可修改的数据。过滤旧事件，忽略不能作为回放水位的 watch 创建响应头。
- **故障监督**：普通断流从 revision+1 恢复，指数退避附带随机抖动；压缩、非法批次重新加载。重载失败继续重载，保留上一份有效快照，不会退回不完整状态。
- **就绪与可观测性**：Status 暴露版本、数量、最近同步、错误与就绪；Observer 提供生命周期事件。etcd 读/watch 使用 WithRequireLeader，定期 List 验证空闲流，超过 MaxStaleness 自动取消就绪。
- **生命周期**：同步完成初始 List，Stop 可取消启动、监听和退避；Done/Err 接入 App 监督。Observer panic 被隔离，后端 panic 终止 Store 并暴露错误。

实现入口为 `discovery/store.go`、`backend.go`、`backend_etcd.go`、`snapshot.go`，完整说明见 [Store 使用文档](discovery-store.md)。原来的低层 Registry.Watch 保留回调及错误返回方式，不自带 Store 的持续退避监督。

## 当前边界

- 已补充 Registrar，监督 key/租约失效并安全重新注册；目录缓存恢复与本实例注册成功仍是独立状态。
- 这不是通用热配置 Store，没有迁入业务 schema、区服/分组关系、配置 CAS 与写事务 API。
- 坏数据拒绝整批并触发全量重载，保留旧快照同时取消就绪；调用方须选择降级或拒绝请求，不能把缓存存在当作健康。
- Snapshot 深复制、变更批次复制目录索引、周期全量读取均有规模成本；Get 只复制一个节点的 Metadata。没有运行吞吐/内存基准，不声称性能更高。
- 已有故障注入和单机 etcd 集成测试不代表完成多节点网络分区、leader 切换与长期负载验证。

## 配套 Discovery 的注意点

mc-rogue 的 Manager 提供节点缓存、标签查询、增删改回调、自动 watch 重启及回调 panic/耗时处理。Orbit 的新 Store 已提供缓存和故障监督；通用 Pool 负责节点增删替换，标签筛选、路由和慢回调策略仍由应用决定，不能单凭接口数量判断底层协议可靠性。

其 `manager.go:173` 采用先 WatchPrefix、再读取快照的方式。锁定依赖的普通 Watch 在后台 goroutine 中才真正建立 watch，并且 Manager 的事件不保留 revision。因此，“WatchPrefix 返回后事件一定已被缓存”的假设不充分；读取快照与开始监听之间可能遗漏更新，回放早于快照的事件也缺少版本过滤。Orbit 的快照 revision+1 方案与 mc-rogue 配套 gameserver Store 的版本化方案更明确。

这是源码时序分析，未执行该竞态的复现实验。底层依赖已经提供版本化接口，Manager 当前适配层仍使用普通 Get/Watch，后续应优先改为版本化路径。

## 推荐演进方向

1. 保留 Orbit 的业务无关 Descriptor、事务占有 ID、租约清理、context 和 revision+1 监听语义。
2. 已从 mc-rogue Store 借鉴后端接口、原子快照、退避恢复、版本信息、可观测性及故障测试思路；未迁入区服/分组/玩家策略。
3. 将远程配置加载与服务发现分成两个能力：远程配置使用通用 Decoder/Validator 和配置 Store；服务发现维护节点身份、租约、存活与路由输入。可以共享 etcd 客户端，但不共享业务模型。
4. 服务发现故障监督与新鲜度已补充，后续按实际需求增加独立的远程配置 Store。业务配置写入需要 CAS 与事务；不应为追求功能数量把所有接口混入 Registry。

## 验证边界

对 mc-rogue 仅做源码、调用链及现有测试审阅，没有修改该项目，也没有运行其测试或性能基准。目标模块未被当前 CodeGraph 索引，gopls 仅按临时单文件包解析，缺少完整外部依赖；因此使用源码及锁定模块缓存进行核对。

Orbit 新增 Store 后已执行 race 单元测试与独立 etcd 集成测试，覆盖原子事务、故障重试、压缩、非法数据修复恢复和所有权；完整检查记录见 [测试说明](testing.md)。比较没有使用未经测试的性能结论。
