# gateway

`Forward(maxConcurrent, route)` 创建有界转发 Handler：Route 选择 Caller 和响应 ID，
Caller 执行后端请求，再把响应序号恢复为原客户端序号。

`request.Client` 实现 Caller；可结合 `discovery.Picker` 选择已连接的 Pool 成员。
Route 负责身份/权限检查、业务路由与 affinity。框架不固定用户、区服、服务类型或协议错误码。

不重试、不复制业务状态、不处理异步推送/离线广播，也不承诺可靠消息投递。
处理错误沿用 Session 的关闭契约；应用可自行转换为响应。每个前端连接的 Handler 串行，
并发上限约束的是跨连接转发，后端响应必须由独立读循环处理。

运行 [两后端网关示例](../examples/gateway/README.md) 验证完整链路。
