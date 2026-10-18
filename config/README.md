# config

加载并校验应用 YAML 配置，避免全局配置和初始化副作用。

- `Load[T]` 读取文件，`Decode[T]` 读取流；类型 T 实现 `Validate() error`，由应用定义字段与业务校验。
- 输入最多 1 MiB，拒绝未知字段、多份 YAML 文档、解码后的 nil 配置和校验失败；调用方必须处理错误后再启动组件。
- 内置 Config 是网络示例的基础配置，支持监听地址、消息上限、队列容量与超时。
- `NewLive(initial, policy)` 提供可选热更新：Reload/ReloadFile 完整校验后原子发布，失败保留旧值与版本，相同内容不增加版本；Snapshot 返回独立的配置数据、版本和更新时间。
- policy 必须显式提供并拒绝需要重启的字段变化；框架不自动重建监听器/连接池。应用在新请求开始时读取一次快照，旧请求可继续使用旧快照。
- 快照类型须能通过 YAML 往返，不放运行句柄或不可序列化对象；Validate/policy 要纯粹、及时返回，不能递归 Reload。并发 Reload 串行，快照读取不等待文件 I/O。普通配置在发布时解析和校验，读取深拷贝已验证的数据；含接口或非导出字段的自定义类型保留 YAML 往返，以免共享隐藏的可变状态（time.Time 可直接复制）。仍建议按请求或批次读取快照，避免重复拷贝。
- 不自动填充默认值、启动文件监听、解析命令行或管理凭据；由应用决定重载触发和错误记录。

参见 [echo 示例](../examples/echo/README.md) 与 [示例配置](../examples/echo/config.yaml)。
热更新与分片任务组合见 [runtime 示例](../examples/runtime/README.md)。
