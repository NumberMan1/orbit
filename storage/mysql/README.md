# storage/mysql

创建经过连通性检查的 SQL 连接池，可选地在现有池上创建 GORM 句柄。

- Open 校验 MaxOpen/MaxIdle 与连接寿命，设置池限制，并在有界 context 下 Ping；失败时关闭已创建的池。
- DSN 未指定时补充拨号、读写超时；凭据及 TLS 等连接配置由应用提供。
- 返回的 `*sql.DB` 归调用方所有，使用 context 化查询、参数化 SQL，及时关闭 Rows，并最终关闭连接池。
- ORM 复用现有池，不转移所有权；GORM 操作使用 WithContext，不隐藏迁表或业务查询。
- 不自动重试业务事务；断线后是否重试需结合幂等性与事务结果判断。

真实数据库验证需设置 `ORBIT_TEST_MYSQL_DSN`，见 [测试说明](../../docs/testing.md)。
