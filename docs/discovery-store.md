# 服务目录 Store

`discovery.Store` 借鉴 mc-rogue `config/gameserver.Store` 的窄后端接口、原子快照、版本化监听、退避恢复和 Observer。这里只保存通用 `Descriptor`，没有引入区服、分组、玩家配置或业务写入策略。它是服务发现的读取缓存；注册和租约仍由 `Registry` 管理。

## 接入与所有权

完整可运行入口见 [examples/discovery](../examples/discovery/main.go)。使用本机开发 etcd：

```sh
go run ./examples/discovery -endpoints http://127.0.0.1:2379
curl -i http://127.0.0.1:9010/readyz
curl http://127.0.0.1:9010/services
```

示例只读取 `/orbit/example/services/`，不写入注册信息；空目录也可以就绪。可在独立开发实例写入演示节点，再观察查询结果：

```sh
etcdctl --endpoints=http://127.0.0.1:2379 put /orbit/example/services/worker-1 \
  '{"id":"worker-1","name":"worker","address":"127.0.0.1:9100"}'
etcdctl --endpoints=http://127.0.0.1:2379 del /orbit/example/services/worker-1
```

演示 Put 没有租约。真实实例使用 `Registry.Register(ctx, descriptor, ttl)`，并监听 `Registration.Done/Err`。Store 不会探测 Address 是否可连接，也不会代替服务自动重新注册。生产环境由应用配置客户端认证、TLS 和独立前缀。

创建方式二选一：

```go
registry, err := discovery.New(client, "/services")
// 检查 err。
store, err := registry.NewStore(discovery.StoreOptions{})
// 检查 err；也可用 NewEtcdBackend + NewStore 注入独立后端。
app, err := service.NewApp(5*time.Second, store, server)
// 检查 err，然后 app.Run(ctx)。
```

Store 放在依赖它的监听器之前，先完成首次 List，再接受请求；App 逆序关闭监听器和 Store，最后由调用方关闭 etcd 客户端。Store 不关闭借用的 Backend/client，不撤销注册租约。`Start` 首次加载失败直接返回错误，不能在同一对象上再次启动；`Stop` 幂等，其 context 限制等待时间。手动启动时也必须停止 Store，再关闭客户端。

## 快照与一致性

- `Get(id)` 返回一个节点及是否存在，复制 Metadata。
- `All()` 返回按 ID 排序、来自同一版本的节点列表。
- `Snapshot()` 返回 `Revision` 和完整 Services 深复制。需要同时读取多个节点时，从同一 Snapshot 查询；多个独立 Get 可能跨版本。
- `Status()` 提供 Revision、Count、LastSync、LastError、Running、Ready。Status 与 Snapshot 是独立读取，不应拼接成“同一时刻”的事务结果。

内部通过 `atomic.Pointer` 发布不可变状态，读取不获取写锁。一个 etcd 事务中的所有节点变更按同一个 revision 一次性发布；先校验整个批次，任意非法记录都会拒绝整批。旧版本/重复事件不会覆盖新快照，创建 watch 的响应头也不会被错误当成回放完成的版本。进度通知只在确认已交付该版本前的全部事件后推进水位。

首次全量读取使用线性一致 Get，再从 `revision+1` 监听。发生 compact 或非法批次时重新 List；重新加载也要通过完整校验和版本不回退检查。加载失败继续重试，保留上一份完整快照；因此坏记录修复前，新数据不会以部分成功的形式发布。

`Get/All/Snapshot` 在故障后仍可读取旧数据。调用方必须结合 `Ready` 决定拒绝请求还是允许降级读取。`Ready` 表示最近成功同步、当前没有已知同步故障且 Store 正在运行，不能保证目录内节点可达，也不能消除健康检查与后续操作之间的时间差。

## 故障恢复与参数

| 参数 | 默认值 | 行为 |
| --- | --- | --- |
| RequestTimeout | 5 秒 | 每次 List 的期限，包括启动和重载 |
| ResyncInterval | 30 秒 | 周期性关闭当前 watch 并重新 List，即使持续有事件也会执行 |
| MaxStaleness | 1 分钟 | 自上次有效同步超过此时长，Ready 自动失效；必须大于 ResyncInterval |
| RetryMin / RetryMax | 100 毫秒 / 5 秒 | 指数退避并添加最多 50% 随机抖动，总等待不超过 RetryMax |
| Logger | slog.Default() | 记录后端或 Observer panic |
| Observer | 无 | 串行收到 loaded、updated、progress、retry、stopped 事件 |

零值参数采用默认值，负值与非法组合返回错误。etcd 读和 watch 使用 `WithRequireLeader`。普通 watch 错误从已发布版本的下一版重连；压缩和坏数据触发全量加载；已进入重载后，即使 List 多次失败也继续重载，不会绕过错误从坏快照接着监听。

周期 List 用于检测安静 watch 的失联，代价是按目录大小产生全量读取和校验开销。不要假设 etcd 默认 progress 通知会在 30 秒内到达。观察到错误会立即取消就绪；没有任何新同步证据时，MaxStaleness 提供独立的过期判定。同步失败记录在 `Status.LastError`；后台仍会恢复，`Err()` 只返回导致 Store 结束的终止错误，供 App 监督。

Observer 应尽快返回，可以读取 Store，但不能同步调用自己的 Stop 等待自身退出。panic 被记录并隔离；缓慢或不返回的 Observer 会延迟同步，Stop 只能按 context 超时返回，Go 不能强制终止回调。需要慢操作时交给应用自己的有界队列。默认未配置 Observer 时不会逐次打印重试错误，应通过 Status 或接入 Observer 观察故障。

## 替换后端与验证

`Backend` 只暴露 List 和 Watch；`Watcher` 只暴露 Next 和 Close。List 必须返回同一版本的完整目录，Watch 从指定版本开始重放，每个 Batch 必须包含该 revision 的完整事务。空 Batch 是已交付水位，不能发送仅代表服务器当前版本的空消息。Watch 应及时返回；Next 必须响应单次调用的 context，包括空闲流上的期限。Close 必须及时、幂等，能与 Next 并发调用。Store 的 RequestTimeout 仅约束 List，不替任意后端提供强制终止能力。

单元测试使用 fake 后端和 Go 1.25 `testing/synctest` 验证退避、重载失败、整批拒绝、空闲失联、取消、Observer 和并发快照隔离。真实 etcd 集成验证版本回放、事务批次、compaction 错误映射、坏数据保留与修复恢复、租约增删及客户端所有权，详见 [测试说明](testing.md)。这些检查不替代多节点网络分区、长时间负载或吞吐基准。
