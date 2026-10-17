# examples

展示框架组合方式，各子目录是独立 main 包。以下示例使用 Go 1.27.2，命令均在仓库根目录执行。

| 示例 | 目的 | 外部依赖 |
| --- | --- | --- |
| [echo](echo/README.md) | TCP/WS 回显、HTTP 健康检查与应用关闭 | 无 |
| [client](client/README.md) | 单次 TCP 发送、接收与会话清理 | 已启动的 echo TCP 服务 |
| [discovery](discovery/README.md) | etcd Store 快照与就绪接口 | 本地或指定 etcd |
| [timers](timers/README.md) | Queue 推进时间轮与回调取消 | 无 |
| [gateway](gateway/README.md) | 两后端网关、限流、请求响应、健康/指标及排空退出 | 无（示例使用静态目录） |

示例强调资源归属和错误处理，不携带游戏协议或业务模型。部署参数、认证和证书需由实际应用配置。
