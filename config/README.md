# config

加载并校验应用 YAML 配置，避免全局配置和初始化副作用。

- `Load[T]` 读取文件，`Decode[T]` 读取流；类型 T 实现 `Validate() error`，由应用定义字段与业务校验。
- 输入最多 1 MiB，拒绝未知字段、多份 YAML 文档和校验失败；调用方必须处理错误后再启动组件。
- 内置 Config 是网络示例的基础配置，支持监听地址、消息上限、队列容量与超时。
- 不自动填充配置默认值、不执行热更新、不解析命令行或管理凭据；这些策略由应用显式处理。

参见 [echo 示例](../examples/echo/README.md) 与 [示例配置](../examples/echo/config.yaml)。
