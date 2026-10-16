# 设计与迁移文档

各包 README 提供职责、并发和资源归属摘要；本目录记录跨包设计、迁移决策及验证依据。

| 文档 | 内容 |
| --- | --- |
| [architecture](architecture.md) | 分层、生命周期、背压与所有权 |
| [framework-extensions](framework-extensions.md) | 消息、连接器、注册/连接池、HTTP 和工具接入 |
| [timers](timers.md) | 标准定时器、PollTimer 与时间轮的选择和边界 |
| [discovery-store](discovery-store.md) | Store 快照、一致性、恢复、就绪与演示 |
| [etcd-comparison](etcd-comparison.md) | 与参考项目的实现对比及借鉴 |
| [migration](migration.md) | 迁移范围、修复与调用方改造步骤 |
| [migration-audit](migration-audit.md) | rocommon 全部 Go 源文件的对应与排除项 |
| [go127](go127.md) | Go 1.27.2、JSON v2 行为与依赖约定 |
| [testing](testing.md) | 检查入口、集成依赖及本机验证记录 |

返回 [项目入口](../README.md) 或查看 [可运行示例](../examples/README.md)。
