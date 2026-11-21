# examples

展示框架组合方式。Go 示例使用 Go 1.27.2，C++ 示例使用 C++23，命令均在仓库根目录执行。

| 示例 | 目的 | 外部依赖 |
| --- | --- | --- |
| [echo](echo/README.md) | TCP/WS 回显、HTTP 健康检查与应用关闭 | 无 |
| [client](client/README.md) | 单次 TCP 发送、接收与会话清理 | 已启动的 echo TCP 服务 |
| [cpp-client](cpp-client/README.md) | C++ 客户端与 Go TCP 服务互通 | 已启动的 echo TCP 服务 |
| [cpp-server](cpp-server/README.md) | C++ TCP 请求服务端及 Go 请求客户端接入 | 无 |
| [discovery](discovery/README.md) | etcd Store 快照与就绪接口 | 本地或指定 etcd |
| [timers](timers/README.md) | Queue 推进时间轮与回调取消 | 无 |
| [gateway](gateway/README.md) | 两后端网关、限流、请求响应、健康/指标及排空退出 | 无（示例使用静态目录） |
| [runtime](runtime/README.md) | 按实体分片执行、配置热更新与版本快照 | 无 |

示例强调资源归属和错误处理，不携带游戏协议或业务模型。部署参数、认证和证书需由实际应用配置。
