# storage/redis

创建 go-redis/v9 的单机或集群客户端，使用 RESP2，支持 Redis 8 集群返回的槽位元数据。调用方显式引用 Redis 类型或错误哨兵时，需要迁移到 `github.com/redis/go-redis/v9`。

- Open 显式选择单机/集群并校验地址；单机恰好一个地址，集群只允许 DB 0。
- 地址切片与 TLS 配置复制后交给客户端，配置拨号/读写超时，使用有界 context 执行启动 Ping。
- Ping 失败会关闭客户端；成功后由调用方持有并最终 Close。每次操作传入合适的 context。
- 默认关闭驱动隐式重试。网络错误可能发生在写入已经提交之后，调用方不能据此判断是否执行过，也不能盲目重试 INCR、LPUSH 等非幂等命令。
- `Retries > 0` 显式启用驱动重放；`ClusterRedirects > 0` 显式启用集群重定向。集群重定向循环也会重试网络错误，因此默认同样关闭。需要自动 MOVED/ASK 跟随的集群必须显式选择重定向次数，并处理重复写风险。负数和单机 ClusterRedirects 配置被拒绝。
- key 设计、缓存一致性、Lua 脚本和业务去重由应用管理；重试配置不等同于业务幂等保证。

真实服务验证可设置 `ORBIT_TEST_REDIS_ADDR` 或逗号分隔的 `ORBIT_TEST_REDIS_CLUSTER_ADDRS`。如需认证，分别设置 `ORBIT_TEST_REDIS_PASSWORD` 或 `ORBIT_TEST_REDIS_CLUSTER_PASSWORD`。
