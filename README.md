# Orbit

[![Go CI](https://github.com/NumberMan1/orbit/actions/workflows/ci.yml/badge.svg)](https://github.com/NumberMan1/orbit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/NumberMan1/orbit.svg)](https://pkg.go.dev/github.com/NumberMan1/orbit)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

通用 Go 服务器框架，使用 **Go 1.27.2**。提供网络会话、报文编解码、事件队列、服务生命周期、服务发现及数据连接能力，不包含具体业务。

项目从 `RO_Server_Trunk-branch_0.1.39/rocommon` 提取并重构；历史迁移与来源说明见 [迁移文档](docs/migration.md) 和 [第三方说明](THIRD_PARTY_NOTICES.md)。

当前迁移是接口重构，不是旧 `rocommon` 的直接替换包。旧报文的明文封装可兼容；旧 RSA/AES 握手需要同步改造，详见 [迁移说明](docs/migration.md)。

## 安装

仓库公开后，或当前已具有仓库读取权限时，可在 Go 项目中添加：

```sh
go get github.com/NumberMan1/orbit@latest
```

生产项目应固定经过验证的 tag 或提交版本。API 文档见 [pkg.go.dev](https://pkg.go.dev/github.com/NumberMan1/orbit)，
发布记录见 [Releases](https://github.com/NumberMan1/orbit/releases)。公开前文档与徽章可能尚不可用。
通用工具与时间轮依赖 `github.com/NumberMan1/numbox v0.4.2`，日志依赖 `github.com/NumberMan1/log v0.3.1`，均通过 Go module 引入。
直接导入 numbox 对应包；旧 util 包已删除，见 [迁移映射](docs/migration.md#直接使用-numbox-v0422026-10-19)。

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

也可使用 C++23 客户端连接同一个 TCP 服务，见 [C++ 回显示例](examples/cpp-client/README.md)。

JSON 直接使用标准库 `encoding/json/v2`，Go 1.27.2 无需手动启用实验开关。v2 的字段匹配、空集合和校验行为见 [适配说明](docs/go127.md)。

## 能力与目录

根包 `orbit` 只定义 `Component` 的 Start/Stop 契约：组件按依赖顺序启动、逆序关闭，Start 失败时自行清理部分资源，Stop 应幂等；除非另有说明，组件为单次使用。各目录 README 简述具体包的并发、资源归属及使用边界。

| 目录 | 能力 |
| --- | --- |
| [rpc](rpc/README.md) | 兼容原明文消息头的帧读写，长度检查、完整读写、序号透传 |
| [cpp](cpp/README.md) | C++23 帧编解码、同步 TCP 客户端与 CMake 接入 |
| [codec](codec/README.md) | 实例级 ID/名称/类型注册表，JSON v2、现代 Protobuf |
| [socket](socket/README.md) | 会话查询/状态、有界收发、类型化消息与回复、收发钩子、队列桥接 |
| [socket/tcp](socket/tcp/README.md) | TCP/TLS、受管理连接器、退避重连、网络选项 |
| [socket/websocket](socket/websocket/README.md) | WebSocket/WSS、受管理连接器、重连、帧与同源检查 |
| [socket/http](socket/http/README.md) | HTTP/HTTPS、私有路由、限量 JSON/表单、状态码响应 |
| [service](service/README.md) | 有序启动、失败回滚、运行故障监督、逆序关闭、串行队列与按实体分片执行 |
| [discovery](discovery/README.md) | etcd 注册/恢复/状态更新、原子 Store、动态节点 Pool、就绪状态 |
| [storage/mysql](storage/mysql/README.md) | 有界 SQL 连接池、可选 GORM，调用方持有并关闭连接 |
| [storage/redis](storage/redis/README.md) | 单机/集群连接、上下文及 TLS、启动检查 |
| [config](config/README.md) | 泛型 YAML 加载、严格校验、带字段策略和版本的原子热更新 |
| [logging](logging/README.md) | NumberMan1/log v0.3.1 接入指南：结构化字段、异步文件输出与生命周期 |
| [health](health/README.md) | 聚合存活/就绪检查，排空期间取消就绪 |
| [request](request/README.md) | 有界请求响应、远端取消/截止时间、心跳应答与失活检测 |
| [observe](observe/README.md) | 可选观测接口、延迟直方图、Prometheus 导出与关联标识 |
| [gateway](gateway/README.md) | 有界后端转发与客户端序号还原 |
| [orbitgen](cmd/orbitgen/README.md) | 消息注册与 Handler 绑定代码生成、一致性检查 |

目录索引：[存储](storage/README.md) · [运行示例](examples/README.md) · [设计文档](docs/README.md) · [CI](.github/workflows/README.md)。

业务自行定义消息、处理函数、路由、身份认证和数据模型。框架没有业务协议生成代码、账号系统、支付配置、游戏逻辑、业务数据库结构或内置密钥。

## 验证

```sh
make verify       # 格式、vet、race 测试、构建；检查精确工具链版本
make fuzz         # 报文与可选请求元数据解码模糊测试
make integration  # 独立 etcd 实例，以及可配置的 MySQL/Redis 集成测试
```

集成测试依赖与实际执行情况见 [测试说明](docs/testing.md)。CI 使用 Go 1.27.2，并配置 MySQL/Redis 测试实例及 Linux、Windows、macOS 编译检查。

进一步阅读：[架构与使用约定](docs/architecture.md) · [迁移对照与已修复问题](docs/migration.md) · [Go 1.27.2 适配](docs/go127.md) · [Store 使用说明](docs/discovery-store.md) · [etcd 实现对比](docs/etcd-comparison.md)。

更多接入：[时间轮与定时器](docs/timers.md) · [补充框架能力](docs/framework-extensions.md) · [rocommon 逐文件核对](docs/migration-audit.md)。可运行 `go run ./examples/timers` 查看时间轮与串行 Queue 的组合。

生产接入：[连接配额、限流、优雅停服、健康与指标](docs/production.md)。
进阶接入：[心跳、熔断、取消传播、观测、分片执行与热更新](docs/reliability.md)。
运行 `go run ./examples/gateway -listen 127.0.0.1:0 -admin 127.0.0.1:0 -self-test`
验证两后端网关、请求响应及排空退出。分阶段停服使用可选 `App.RunGraceful`，原有 Run/Close 行为保持兼容。

## 参与与许可

[贡献指南](CONTRIBUTING.md) · [行为准则](CODE_OF_CONDUCT.md) · [获取帮助](SUPPORT.md) ·
[安全政策](SECURITY.md) · [变更记录](CHANGELOG.md) · [维护与发布](MAINTAINERS.md)。

项目保留 [MIT 许可证](LICENSE)。第三方依赖保留自身许可；历史迁移内容的来源与公开前核对事项见
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
