# Orbit

从 `RO_Server_Trunk-branch_0.1.39/rocommon` 提取并重构的通用 Go 服务器框架，目标版本为 **Go 1.27.2**。保留网络会话、报文编解码、事件队列、服务生命周期、服务发现及数据连接能力，不包含具体业务。

当前迁移是接口重构，不是旧 `rocommon` 的直接替换包。旧报文的明文封装可兼容；旧 RSA/AES 握手需要同步改造，详见 [迁移说明](docs/migration.md)。

## 快速运行

在仓库根目录，使用 Go 1.27.2：

```sh
go version
go mod download
go run ./examples/echo -config examples/echo/config.yaml
```

另开终端：

```sh
go run ./examples/client
curl http://127.0.0.1:9001/healthz
```

客户端输出 `id=1 sequence=42 payload=hello orbit`。示例还在 `127.0.0.1:9002` 提供 WebSocket 二进制回显。按 Ctrl+C 退出服务器会关闭监听器、连接和后台任务。示例不依赖 etcd、MySQL 或 Redis，配置中的三个监听地址可分别留空禁用，但至少保留一个。

JSON 直接使用标准库 `encoding/json/v2`，Go 1.27.2 无需手动启用实验开关。v2 的字段匹配、空集合和校验行为见 [适配说明](docs/go127.md)。

## 能力与目录

| 目录 | 能力 |
| --- | --- |
| `rpc` | 兼容原明文消息头的帧读写，长度检查、完整读写、序号透传 |
| `codec` | 实例级 ID/名称/类型注册表，JSON v2、现代 Protobuf |
| `socket` | 会话查询/状态、有界收发、类型化消息与回复、收发钩子、队列桥接 |
| `socket/tcp` | TCP/TLS、受管理连接器、退避重连、网络选项 |
| `socket/websocket` | WebSocket/WSS、受管理连接器、重连、帧与同源检查 |
| `socket/http` | HTTP/HTTPS、私有路由、限量 JSON/表单、状态码响应 |
| `service` | 有序启动、失败回滚、运行故障监督、逆序关闭、串行事件队列 |
| `discovery` | etcd 注册/恢复/状态更新、原子 Store、动态节点 Pool、就绪状态 |
| `storage/mysql` | 有界 SQL 连接池、可选 GORM，调用方持有并关闭连接 |
| `storage/redis` | 单机/集群连接、上下文及 TLS、启动检查 |
| `config`、`util` | 严格 YAML、时间轮、可暂停计时器、位图、Unicode 匹配、完整写入 |
| `logging` | slog 的可选限量文件轮转输出 |

业务自行定义消息、处理函数、路由、身份认证和数据模型。框架没有业务协议生成代码、账号系统、支付配置、游戏逻辑、业务数据库结构或内置密钥。

## 验证

```sh
make verify       # 格式、vet、race 测试、构建；检查精确工具链版本
make fuzz         # 报文解码模糊测试
make integration  # 独立 etcd 实例，以及可配置的 MySQL/Redis 集成测试
```

集成测试依赖与实际执行情况见 [测试说明](docs/testing.md)。CI 使用 Go 1.27.2，并配置 MySQL/Redis 测试实例及 Linux、Windows、macOS 编译检查。

进一步阅读：[架构与使用约定](docs/architecture.md) · [迁移对照与已修复问题](docs/migration.md) · [Go 1.27.2 适配](docs/go127.md) · [Store 使用说明](docs/discovery-store.md) · [etcd 实现对比](docs/etcd-comparison.md)。

更多接入：[时间轮与定时器](docs/timers.md) · [补充框架能力](docs/framework-extensions.md) · [rocommon 逐文件核对](docs/migration-audit.md)。可运行 `go run ./examples/timers` 查看时间轮与串行 Queue 的组合。
