# storage

存储连接入口的目录集合，本目录本身没有 Go 包。

- [mysql](mysql/README.md)：校验连接池、启动 Ping、标准 SQL 与可选 GORM。
- [redis](redis/README.md)：单机/集群客户端、context、TLS 与启动 Ping。
- 连接由调用方显式创建并关闭，操作复用驱动原生接口；数据模型、表结构、事务策略和业务重试留在应用层。

外部服务验证方式见 [测试说明](../docs/testing.md)。
