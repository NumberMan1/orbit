# observe

提供可选观测契约和小型固定基数指标导出器，不安装全局注册器或后台上报任务。

- Observer 接收 Handler、QueueWait、ClientCall、ServerCall 事件；可以组合指标与应用的日志/追踪适配器。Emit 隔离 panic，观察者必须并发安全并及时返回。
- socket.Options.Observer 记录分发及异步/排队回调执行，Queued 单独记录排队等待；request.Options/ServerOptions.Observer 分别记录调用等待及实际请求处理时间。
- Metrics 零值可用，只保留四种事件各一组固定延迟桶和错误数；不把消息 ID、关联 ID、节点、错误文本做标签，内存和指标基数固定。
- 将 Metrics 作为 http.Handler 显式挂到私有 mux，输出 [Prometheus text 0.0.4](https://prometheus.io/docs/instrumenting/exposition_formats/) 的 histogram/counter。它与 socket.Metrics 的传输计数互补；不是指标存储服务，也不宣称已接入远程监控平台。
- Handler 时间包含包装器分发和实际执行，不能当作端到端延迟；ClientCall 更适合衡量调用体验，ServerCall 衡量请求执行，QueueWait 衡量排队。不要把它们直接相加，可能重复计时。
- TraceID/WithTraceID/EnsureTrace 管理受限格式的关联标识；request 的可选元数据在网关/后端间传播。没有内置采样器、span 树或 OTLP 导出器，应用可通过 Observer 桥接已有追踪系统。

示例见 [gateway](../examples/gateway/README.md)，接口与部署边界见 [运行扩展](../docs/reliability.md)。
