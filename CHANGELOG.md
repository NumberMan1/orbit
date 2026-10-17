# 变更记录

记录面向使用者的变更。只有实际发布后才填写版本与发布日期；历史详情以
[Tags](https://github.com/NumberMan1/orbit/tags) 和 [提交记录](https://github.com/NumberMan1/orbit/commits) 为准。

## 未发布

### 服务器运行能力

- 增加共享连接配额、每来源连接限制、Session 入站令牌桶和处理中请求并发限制；默认保持禁用。
- 增加 Session/TCP/WS/HTTP 排空接口与可选 App.RunGraceful，跟踪 Queued 中已接受请求；原有 Run/Close 保持立即关闭语义。
- 增加 health 组件、Session/Queue/Admission/request 状态和可共享的无全局注册运行指标。
- 增加 request 调用层：有界等待表、唯一序号、默认超时、乱序匹配、断连/取消清理；不自动重试。
- 增加 discovery.Picker 的健康连接选择、轮询和 key 亲和路由，以及 gateway.Forward 的有界转发与序号还原。
- 增加 orbitgen 注册代码生成、生成物一致性检查和两后端网关端到端示例。
- 配置热更新、完整 Actor、AOI、异步推送和持久化消息交付不在本轮范围；通用层不引入账户/支付/玩家模型。

### 依赖与时间轮

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
