# storage/redis

创建 go-redis/v8 的单机或集群客户端，保留原生命令接口。

- Open 显式选择单机/集群并校验地址；单机恰好一个地址，集群只允许 DB 0。
- 地址切片与 TLS 配置复制后交给客户端，配置拨号/读写超时，使用有界 context 执行启动 Ping。
- Ping 失败会关闭客户端；成功后由调用方持有并最终 Close。每次操作传入合适的 context。
- key 设计、缓存一致性、Lua 脚本和业务重试由应用管理；驱动的重试行为不等同于业务幂等保证。

真实服务验证需设置 `ORBIT_TEST_REDIS_ADDR`，见 [测试说明](../../docs/testing.md)。
