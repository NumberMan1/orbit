# health

显式挂载的存活和就绪检查，使用示例见 [网关示例](../examples/gateway/README.md)。

- `New(timeout)` 创建检查集合；`Add(name, check)` 注册依赖，不允许重名。
- 作为 App 组件 Start 后存活，Quiesce 后不就绪，Stop 后不存活；不能重用。
- `Live()` 不检查依赖；`Probe(ctx)` 检查依赖和生命周期状态。
- `Liveness`、`Readiness` 是供私有 mux 挂载的 HandlerFunc；失败返回 503。
- 检查必须并发安全并遵守 context；总预算覆盖全部检查。panic 视为失败，不将原始错误写入响应。
- 没有自动暴露的公共管理端点。健康检查应结合实际业务依赖配置，不能把 TCP 已连接等同于业务就绪。

详见 [生产能力说明](../docs/production.md)。
