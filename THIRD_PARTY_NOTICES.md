# 第三方来源与许可说明

根目录 [LICENSE](LICENSE) 用于本项目有权授权的原创内容，不替代第三方内容的版权与许可证。
本文件记录依赖及历史来源核对事项，不代表已经完成全部历史内容的许可审计。

## Go module 依赖

版本与校验值以 [go.mod](go.mod) 和 [go.sum](go.sum) 为准。直接依赖包括：

| 模块 | 用途 |
| --- | --- |
| `github.com/NumberMan1/numbox` | `pkg/timewheel` 时间轮、`pkg/matcher` 版本回归 |
| `github.com/NumberMan1/log` | 结构化日志与输出生命周期 |
| `github.com/go-redis/redis/v8` | Redis 客户端 |
| `github.com/go-sql-driver/mysql` | MySQL 驱动 |
| `github.com/gorilla/websocket` | WebSocket 传输 |
| `go.etcd.io/etcd/api/v3`、`go.etcd.io/etcd/client/v3` | etcd 服务发现 |
| `google.golang.org/protobuf` | Protobuf 编解码 |
| `gopkg.in/yaml.v3` | YAML 配置 |
| `gorm.io/driver/mysql`、`gorm.io/gorm` | 可选 ORM 接入 |

间接依赖也保留各自的版权与许可。依赖某个模块不表示使用了其中所有包；
Orbit 直接使用 numbox 的 `pkg/timewheel`，并对 `pkg/matcher` 保留版本回归测试，
不复制其算法；[上游 v0.4.2](https://github.com/NumberMan1/numbox/tree/v0.4.2) 采用 MIT 许可，
版权与授权条款见该版本的 [LICENSE](https://github.com/NumberMan1/numbox/blob/v0.4.2/LICENSE)。
发布包含依赖源码的 vendor、压缩包或其他再分发物时，应保留对应版本的 LICENSE、NOTICE 等文件。

## 历史迁移来源

Orbit 从 `RO_Server_Trunk-branch_0.1.39/rocommon` 提取和重构；逐文件映射见
[迁移核对表](docs/migration-audit.md)，接口与协议变更见 [迁移说明](docs/migration.md)。
公开发布前，维护者需要结合原始代码和引入记录确认迁移内容的权属、可再分发授权及应保留的版权声明。
现有 MIT 文件本身不能证明历史源项目的全部内容均可按 MIT 再授权；不要猜测原作者、年份或许可。

numbox 时间轮在上游记录了来源于 Orbit 提交 `81cbe7b`。当前 Orbit 通过发布 module 使用该实现，
不在本仓库维护第二份算法。后续直接引入第三方代码或数据时，须在引入时记录上游地址、版本、修改范围和许可。
