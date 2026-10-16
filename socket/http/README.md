# socket/http

封装标准 net/http 的服务器生命周期及限量 JSON/表单请求，不将 HTTP 请求包装成长连接会话。

- Server 使用显式 handler/私有 mux，配置读写及头部超时；Stop 优雅 Shutdown，失败后强制 Close。父 context 用于初始化，关闭需调用 Stop。
- DecodeJSON 限制正文大小，使用 JSON v2，拒绝未知字段、重复字段、非法 UTF-8 和尾随数据。
- DecodeForm 只处理 URL-encoded 正文，保留重复值，不混入查询参数；Multipart 使用标准库与应用自定限制。
- WriteJSON 在提交响应头前编码，保留指定状态码并返回写入错误；无正文状态直接使用 ResponseWriter。
- NewClient、DoJSON、DoForm 支持 context、总超时和响应上限，非 2xx 返回错误。调用方复用客户端，并在关闭时调用 CloseIdleConnections。
- TLS、认证、路由和请求数据结构由应用配置；没有全局路由或隐式调试接口。

参见 [JSON v2 语义](../../docs/go127.md) 与 [HTTP 接入](../../docs/framework-extensions.md)。
