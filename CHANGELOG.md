# 变更记录

记录面向使用者的变更。只有实际发布后才填写版本与发布日期；历史详情以
[Tags](https://github.com/NumberMan1/orbit/tags) 和 [提交记录](https://github.com/NumberMan1/orbit/commits) 为准。

## 未发布

### 审查修复

- request.Server 等待 Queued/Async 实际完成，保留请求 context 和并发名额；Session 断连后先等待已接受工作，再执行 OnClose/清理状态。Drain 继续接收控制/响应帧并跟踪出站 Call；心跳正常退出排空。
- Picker/Balancer 为每个服务独立轮询；临时未就绪保留熔断冷却，成员变更/断线清理旧状态。路由目录只在变化时复制和排序。
- Redis 默认禁用隐式命令重放，新增显式 Retries/ClusterRedirects；集群 MOVED/ASK 跟随需显式启用，并承担网络错误重试可能重复写的风险。
- 配置拒绝 nil 解码结果；Live 普通数据快照直接深拷贝已校验值，带隐藏状态的类型保留 YAML 隔离。
- HTTP 自定义默认 RoundTripper 返回错误而非 panic，增加借用传输的 NewClientWithTransport；orbitgen 避开局部参数与导入/类型重名，提前拒绝生成声明冲突。
- util 的 Bitmap、Matcher、PollTimer、After/Every、WriteFull 收敛为 numbox v0.4.1 兼容入口，保留现有 API。

### 故障处理与运行扩展

- 增加 request 心跳应答、连续超时判定、RTT 与显式失活断线选项，共用一个 Client 的唯一序号空间。
- 增加 discovery.Balancer 的连续失败熔断、冷却和单请求恢复探测；迟到结果不能关闭新一轮熔断。`orbit.draining=true` 只更新路由状态，不重建已有连接。
- 增加可选 request.Server、远端取消控制帧与版本化请求元数据，传播截止时间和关联 ID；原始帧头和默认 Client 载荷不变。socket.Async 有界执行并参与排空。
- 增加 gateway.ForwardWithOptions 的请求级错误映射，允许错误回包后保留连接；不隐式重试。
- 增加 observe 事件接口、固定基数的延迟直方图及私有 Prometheus 导出；记录队列等待、请求执行和调用耗时，隔离观测回调 panic。
- 增加 service.Shards：固定数量、有界队列的按实体串行执行器；增加 config.Live：完整校验、显式字段策略、原子快照和版本查询。
- 网关示例接入心跳/熔断/元数据/取消/指标；增加 runtime 示例演示分片任务与配置热更新。

### 服务器运行能力

- 增加共享连接配额、每来源连接限制、Session 入站令牌桶和处理中请求并发限制；默认保持禁用。
- 增加 Session/TCP/WS/HTTP 排空接口与可选 App.RunGraceful，跟踪 Queued 中已接受请求；原有 Run/Close 保持立即关闭语义。
- 增加 health 组件、Session/Queue/Admission/request 状态和可共享的无全局注册运行指标。
- 增加 request 调用层：有界等待表、唯一序号、默认超时、乱序匹配、断连/取消清理；不自动重试。
- 增加 discovery.Picker 的健康连接选择、轮询和 key 亲和路由，以及 gateway.Forward 的有界转发与序号还原。
- 增加 orbitgen 注册代码生成、生成物一致性检查和两后端网关端到端示例。
- 通用层不引入账户/支付/玩家模型；完整 Actor、AOI、异步推送路由和持久化消息交付仍由应用或后续扩展提供。

### 依赖与时间轮

- numbox 发布依赖由 v0.3.0 升至 v0.4.1；时间轮 key 校验改用无反射比较，公开接口与日志适配保持兼容。补充嵌套接口及 NaN key 的拒绝回归。
- 时间轮改用 `github.com/NumberMan1/numbox v0.3.0` 的 `pkg/timewheel`，移除 Orbit 内的调度算法。
- 保留 `util.TimeWheel[K]` 类型别名、构造入口与 `WheelOptions.Logger`，将上游 slog 诊断转交应用的 NumberMan1/log Logger；组件仍不拥有日志 core。
- 错误变量复用上游哨兵，新增 `util.ErrInvalidKey`；错误文本前缀由 `util:` 改为 `timewheel:`，调用方应使用 `errors.Is`。
- 不可比较的接口 key 和包含 NaN 的 key 被拒绝；空轮 Advance 可直接追到目标时间，不消耗逐槽追赶预算。
- `github.com/NumberMan1/log` 从 v0.3.0 升至 v0.3.1，并同步其必要间接依赖。

### 开源协作准备

- 补充贡献指南、行为准则、安全政策、支持入口、维护发布流程和第三方来源说明；保留现有 MIT LICENSE。
- 补充 Issue/PR 模板、CODEOWNERS、Dependabot、编辑器与换行配置及仓库协作约定。
- 完善 CI 的手动触发、超时和并发取消，保留协议 fuzz、跨平台构建与基础设施集成检查。
- 整理 README 安装、API 文档、许可证与社区入口。仓库可见性与安全报告等管理设置由维护者在 GitHub 配置。
