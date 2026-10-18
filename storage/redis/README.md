# storage/redis

创建 go-redis/v8 的单机或集群客户端，保留原生命令接口。

- Open 显式选择单机/集群并校验地址；单机恰好一个地址，集群只允许 DB 0。
- 地址切片与 TLS 配置复制后交给客户端，配置拨号/读写超时，使用有界 context 执行启动 Ping。
- Ping 失败会关闭客户端；成功后由调用方持有并最终 Close。每次操作传入合适的 context。
- 默认关闭驱动隐式重试。网络错误可能发生在写入已经提交之后，调用方不能据此判断是否执行过，也不能盲目重试 INCR、LPUSH 等非幂等命令。
- `Retries > 0` 显式启用驱动重放；`ClusterRedirects > 0` 显式启用集群重定向。在 go-redis/v8 中，集群重定向循环也会重试网络错误，因此默认同样关闭。需要自动 MOVED/ASK 跟随的集群必须显式选择重定向次数，并处理重复写风险；负数和单机 ClusterRedirects 配置被拒绝。
- key 设计、缓存一致性、Lua 脚本和业务去重由应用管理；重试配置不等同于业务幂等保证。

真实服务验证需设置 `ORBIT_TEST_REDIS_ADDR`，见 [测试说明](../../docs/testing.md)。
