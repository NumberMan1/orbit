# discovery

`NewPicker(pool)` 从目录就绪且已连接的节点中选择；空 affinity 使用轮询，非空 affinity 使用稳定 ID 的 rendezvous hash。
Pool 成员需实现 `SessionProvider`（Connector 已实现），自定义组件可加入业务就绪条件。选择后仍可能断线，不自动重试。
`NewBalancer(pool, options)` 提供独立的熔断选择器，使用 Acquire 返回 Selection，并在每次调用完成或路由初始化失败时调用 Complete(err)。连续失败达到阈值后冷却，随后只允许一个恢复探测；新 Session 使用新状态，旧请求结果不能关闭新一轮熔断。
默认失败分类排除调用方 context.Canceled；应用应按业务定义 Failed，排除业务错误或本地过载。半开探测被取消时重新冷却，不把取消视为恢复成功。选择与发送之间仍可能断线；不自动重试或重放。
Picker/Balancer 排除本地正在 Drain 的 Session 和目录中 `orbit.draining=true` 的节点。单独修改此元数据不重建 Pool 连接；其他描述符变化仍按原替换契约处理。目录传播有延迟，不保证零新增请求，应与本机 Quiesce/Drain 配合。
轮询游标按服务名独立维护，交替选择多个服务不会饿死某些节点；就绪标记短暂变为 false 不会清除仍存活 Session 的熔断冷却。Pool 缓存按 ID 排序的路由目录，仅在成员或路由元数据变化时重建；普通轮询无需每次复制和排序全量节点。SessionProvider.Session 必须及时返回且不重入选择器。
详见 [生产能力说明](../docs/production.md) 和 [网关示例](../examples/gateway/README.md)。

通过 etcd 管理通用服务目录，将注册、读取缓存和节点连接生命周期分开。StoreOptions.Logger 接收 `NumberMan1/log` 的 Logger，日志 core 由应用管理，见 [日志接入](../logging/README.md)。

- Descriptor 仅包含 ID、名称、地址和元数据；区服筛选、路由与认证由应用决定。
- Registry 通过事务独占 ID，Registration 持续续租并监听 key 所有权；Update 校验原租约，防止覆盖后继实例。
- Registrar 首次同步注册，运行中丢失注册后退避重试；不会抢占仍被其他实例持有的 ID。
- Store 依赖窄 Backend 接口，按 revision 批次原子发布快照，读取返回副本；坏数据保留旧快照并取消就绪，支持重载与过期检测。
- Pool 在 Store 启动后协调应用工厂创建的组件，先停止旧节点再替换；目录异常时保留节点，连接可用性仍由 Connector 判断。
- etcd 客户端归调用方所有，停止组件后再关闭客户端。Store 的 Status().Ready 仅表示目录近期同步正常，不保证目录中的服务可连接。

参见 [Store 设计](../docs/discovery-store.md)、[注册与连接池接入](../docs/framework-extensions.md)、[实现对比](../docs/etcd-comparison.md) 和 [示例](../examples/discovery/README.md)。
