# discovery

通过 etcd 管理通用服务目录，将注册、读取缓存和节点连接生命周期分开。StoreOptions.Logger 接收 `NumberMan1/log` 的 Logger，日志 core 由应用管理，见 [日志接入](../logging/README.md)。

- Descriptor 仅包含 ID、名称、地址和元数据；区服筛选、路由与认证由应用决定。
- Registry 通过事务独占 ID，Registration 持续续租并监听 key 所有权；Update 校验原租约，防止覆盖后继实例。
- Registrar 首次同步注册，运行中丢失注册后退避重试；不会抢占仍被其他实例持有的 ID。
- Store 依赖窄 Backend 接口，按 revision 批次原子发布快照，读取返回副本；坏数据保留旧快照并取消就绪，支持重载与过期检测。
- Pool 在 Store 启动后协调应用工厂创建的组件，先停止旧节点再替换；目录异常时保留节点，连接可用性仍由 Connector 判断。
- etcd 客户端归调用方所有，停止组件后再关闭客户端。Store 的 Status().Ready 仅表示目录近期同步正常，不保证目录中的服务可连接。

参见 [Store 设计](../docs/discovery-store.md)、[注册与连接池接入](../docs/framework-extensions.md)、[实现对比](../docs/etcd-comparison.md) 和 [示例](../examples/discovery/README.md)。
