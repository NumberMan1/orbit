# 测试与运行验证

## 常规检查

使用精确的 Go 1.27.2，JSON v2 使用工具链默认配置，无需设置实验开关：

```sh
make verify
make fuzz
```

测试覆盖：明文协议固定字节向量、截断/超限/flags、零进展与短写、编解码注册原子性、并发发送/关闭、队列背压/排空/panic、启动回滚和运行故障监督、定时取消、TCP 与 TLS 回显、重连取消和新 Session、HTTP 生命周期与大小限制、WebSocket 同源检查/非法包/已升级连接回收，以及 Store 事务原子性、快照复制、退避/反复重载失败、坏数据拒绝、就绪过期、周期同步、启动取消、Observer 隔离和后端 panic 关闭。

包内测试无需访问外部服务。TCP、HTTP、WebSocket 测试会绑定回环随机端口；受限环境中需允许本机监听。`-race` 用于支持竞态检测的平台。Fuzz 的默认时间为 10 秒，可增加时间验证：

```sh
go test ./rpc -run='^$' -fuzz=FuzzRead -fuzztime=60s
```

## 基础设施集成

安装 etcd 可执行文件并确保它在 PATH 中。测试自行启动一个使用临时数据目录和回环随机端口的进程，结束后销毁，不接触已有集群。验证续租超过初始 TTL、重复 ID 事务保护、查询/watch 窗口更新、租约丢失和撤销；Store 额外验证事务版本批次、历史版本压缩错误、坏数据不污染旧快照、修复后自动重载、注册节点增删以及停止后客户端仍可使用。

MySQL/Redis 需要明确指定独立测试实例。下列凭据仅为本地测试示例：

```sh
ORBIT_TEST_MYSQL_DSN='root:orbit-test-only@tcp(127.0.0.1:3306)/orbit_test?parseTime=true' \
ORBIT_TEST_REDIS_ADDR='127.0.0.1:6379' \
make integration
```

MySQL 测试只执行 SELECT 与 GORM 读取，Redis 测试执行 PING。没有 etcd 二进制或未指定对应存储环境变量时，相应测试明确 skip。可用 `go test -v -tags=integration ./discovery ./storage/...` 查看 skip 原因。CI 提供 MySQL 8.4、Redis 7.4 和 etcd 3.6.7，因此会实际执行这些基础测试。

不把单机连接测试等同于集群验证。上线前还应使用目标版本与真实拓扑验证 Redis 集群重定向、etcd 多节点故障切换/压缩期间恢复、MySQL 断网与事务行为、认证/TLS 证书轮换、长时间负载、慢消费者和应用级 ACK。

## Go 1.27.2 与 JSON v2 的本机验证

使用官方 Go 1.27.2 darwin/arm64 工具链，下载包按官方发布列表的 SHA256 校验。测试时清空 GOEXPERIMENT，确认直接导入 encoding/json/v2、构建和运行均不需要手动启用实验开关。

- `make verify` 通过：对应工具链的 gofmt、go vet、全量 race 测试及构建。
- `make integration` 通过：真实 etcd 隔离实例验证注册、事务快照、坏数据恢复、压缩和所有权。MySQL/Redis 没有配置本机测试实例，相关实例测试跳过；未将跳过计为实际数据库验证。
- `make fuzz` 运行 10 秒，约 158 万次执行，没有失败输入。该数字仅为本次执行记录，不用于比较工具链性能。
- `go mod verify` 通过，`go mod tidy -diff` 无差异。
- Linux amd64、Windows amd64 使用 CGO_ENABLED=0 交叉构建通过，macOS arm64 原生构建通过。
- JSON v2 回归测试覆盖重复字段（含转义同名、嵌套重复）、非法 UTF-8、大小写匹配、nil 集合、omitempty/omitzero、HTTP 尾随数据、正文上限和读取错误传播。
- 实际运行 discovery 示例：快照 JSON 返回正确；重复 id 字段使就绪接口返回 503；修复后恢复 200；SIGTERM 退出码为 0，监听端口关闭。

CI 已更新为 Go 1.27.2 默认配置，本机结果不等同于远程 CI 结果。

## Go 1.25.1 迁移阶段的本机验证

此前迁移阶段的工具链为 Go 1.25.1，宿主为 macOS arm64。常规测试与 race 已执行；etcd 使用本机 3.6.12 二进制启动隔离实例并实际验证。MySQL/Redis 本机没有配置测试地址，外部服务集成测试跳过，仅验证参数校验与构建；CI 流程已配置，但本次本机工作不会宣称远程 CI 已运行。

该阶段本机检查结果：

- `make verify` 通过：格式、`go vet`、全量 `go test -race`、构建。
- 报文模糊测试运行 10 秒，执行约 352 万次，没有发现失败输入。
- `go mod verify` 通过，`go mod tidy -diff` 没有差异。
- Linux amd64（`CGO_ENABLED=0`）、Windows amd64 交叉构建通过，macOS arm64 原生构建通过。macOS 本机 C 编译器不能直接构建 Linux 的 cgo；本框架不依赖 cgo，交叉构建使用纯 Go 模式。
- 实际运行 echo/client 示例：TCP 序号和内容正确返回，HTTP 健康检查成功，SIGTERM 返回退出码 0，三个监听端口都停止接受连接并可按 Go 的 SO_REUSEADDR 规则重新绑定。
- Store 增补后重新运行 `make verify` 与 `go test -race -tags=integration -timeout=90s ./discovery`，均通过；实际运行 discovery 示例验证空目录就绪、写入可见、坏 JSON 触发 HTTP 503、修复后恢复 HTTP 200，以及 SIGTERM 正常退出并关闭监听。

测试不能保证所有回调或任意应用业务都没有数据竞争；业务自行保证跨连接共享状态的同步。
