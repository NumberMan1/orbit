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

## rocommon 补齐验证（2026-10-16）

继续使用 Go 1.27.2 darwin/arm64，清空 GOEXPERIMENT；源项目只读。

- 逐文件核对表与源目录的 47 个 Go 文件集合完全一致，并检查文档中的本地链接。文件覆盖不等于旧 API 兼容，各项替代与排除边界见 [迁移核对表](migration-audit.md)。
- `make verify` 通过：格式、vet、全量 race 与构建。新增测试覆盖时间轮整圈/取整/追赶/回调修改、暂停恢复、位图/Unicode 匹配、会话状态/自定义 ID/发送钩子/同步接收、TCP/WS 重连、表单/响应状态和并发日志轮转。
- `go test -race -tags=integration -timeout=90s ./discovery` 通过：临时真实 etcd 验证 key 删除后重注册、后继实例所有权保护、带租约更新；新增完整链路验证注册 → Store → Pool → TCP Connector 收发，以及移除注册后连接器和服务端会话关闭。Pool 单元测试另覆盖替换、目录异常保留节点、启动清理失败时禁止重复创建。
- `go run ./examples/timers` 实际触发三次后正常退出。时间轮与 runtime 定时器的维护基准已执行，数据及适用边界见 [时间轮说明](timers.md)；未将局部基准作为游戏负载性能结论。
- Linux amd64、Windows amd64 的 `CGO_ENABLED=0` 交叉构建通过；`go mod verify` 通过、`go mod tidy -diff` 无差异。

本轮没有配置 MySQL/Redis 实例，未追加真实数据库验证；远程 CI 结果未在本机验证中计算。

## NumberMan1/log v0.3.0 切换验证（2026-10-16）

上述自有日志轮转测试属于切换前的验证记录；当时已删除本地 RotatingWriter，切换至发布 tag v0.3.0。Go 仍为 1.27.2，GOEXPERIMENT 为空。

- `make verify` 通过：格式、vet、全量 race 与构建。根目录 `logging_test.go` 验证 Queue/TimeWheel/Session 的 panic 日志字段、错误对象保留、借用 core 不被组件关闭，以及异步文件输出在关闭时排空。
- 对所固定日志依赖执行了 7 项针对性 race 回归：禁用级别幂等、并发配置/打印、并发启用不丢更新、合法 JSON 兜底、写入故障不 panic、队列溢出报告、并发写入与关闭排空，全部通过；没有将此结果描述成上游全库测试。
- `go test -race -tags=integration -timeout=90s ./discovery` 通过，验证日志及间接依赖更新后的真实 etcd 注册、恢复和连接池链路。
- Linux amd64、Windows amd64 的 `CGO_ENABLED=0` 交叉构建通过；macOS arm64 已原生运行上述测试。交叉构建不等同于目标平台原生运行测试。
- 实际运行 timers，确认三次 pulse、合法 JSON 结构化字段及正常退出；echo 使用不存在的配置启动，确认输出结构化错误日志并以失败状态退出。
- `go mod verify` 通过，`go mod tidy -diff` 无差异；文档本地链接检查通过。Sentry 未配置外部上报，MySQL/Redis 未追加真实实例验证。

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

## numbox 时间轮与 log v0.3.1 更新验证（2026-10-17）

本轮环境为 Windows amd64、Go 1.27.2，依赖固定 numbox v0.3.0 和 log v0.3.1。

- `go test -count=1 -timeout=90s ./...` 通过，包含时间轮边界、取消/重设、panic 隔离和网络传输用例。首次沙箱运行因回环连接权限失败；允许本机网络后全量重跑通过。
- 新增回归验证上游类型兼容、错误哨兵、非法 key、nil key 和空轮快速推进；日志适配验证默认 core 快照、原始 panic/error 对象、字段作用域与级别映射。
- `go run ./examples/timers` 实际输出三次结构化 pulse 日志后正常退出。
- `go vet ./...`、`go build ./...`、`go mod verify` 通过；`go mod tidy -diff` 无差异。
- 协议 fuzz 实际运行 10 秒，通过约 262 万次执行，无失败输入；此数字不作为性能比较依据。
- `gofmt -l .` 无输出，`git diff --check` 通过；GitHub YAML 语法与文档本地文件链接检查通过。
- 尝试 `CGO_ENABLED=1 go test -race -count=1 -timeout=90s ./...`，因本机缺少 gcc 在编译阶段失败，未完成竞态验证。CI 保留 Ubuntu 上的 race 检查，未将未运行的远端 CI 算作通过。

本轮未追加 etcd、MySQL、Redis 真实实例集成验证，也未变更 GitHub 仓库可见性或创建发布 tag。

## 服务器运行能力补齐验证（2026-10-17）

环境为 Windows amd64、Go 1.27.2；新增能力与适用边界见 [生产服务基础能力](production.md)。

- `go test -count=1 -timeout=90s ./...` 通过；新增测试覆盖 TCP/WS 连接配额释放、消息限流、并发配额、已接收请求排空回包、强制关闭超时、停服阶段顺序、健康检查隔离、请求超时/断线/乱序/迟到响应、节点筛选与黏性选择、生成器校验及完整网关链路。
- 实际运行 gateway 示例的 `-self-test`：四次请求轮询到两台后端，响应内容和序号正确，三个管理端点正常，随后排空退出。补充 SIGTERM 支持后，网关端到端测试重新通过；Windows 本机未实际发送 Unix SIGTERM。
- `go vet ./...`、Windows 原生 `go build ./...`、`go mod verify` 通过，`go mod tidy -diff` 无差异；Linux amd64 与 macOS arm64 的 `CGO_ENABLED=0` 交叉构建通过，未将交叉构建视为目标平台原生测试。
- 协议 fuzz 运行 10 秒，通过约 133 万次执行，无失败输入；生成器 `-check` 通过，已加入 `make verify`。本次执行量不作为性能比较依据。
- `gofmt -l .` 无输出，`git diff --check` 与文档本地文件链接检查通过。
- 尝试 `CGO_ENABLED=1 go test -race -count=1 -timeout=90s ./...`，因缺少 gcc 在编译阶段失败；竞态验证未完成，不能将本次检查视为完整 `make verify` 通过。
- 执行 `go test -v -tags=integration -timeout=90s ./discovery ./storage/...`；非外部实例用例通过。五项真实 etcd 用例因缺少 etcd 可执行文件跳过，MySQL/Redis 各一项实例用例因未配置测试连接跳过，未完成真实基础设施集成验证。

网关示例使用进程内静态目录及真实回环 TCP/HTTP，不依赖外部服务。远程 CI、持续负载与生产部署验证未在本轮执行。

## 补充完成竞态与真实实例验证（2026-10-17）

前述 gcc 与服务缺失阻碍已解决。本次使用 Windows amd64、Go 1.27.2、WinLibs GCC 16.2.0，显式设置 `CGO_ENABLED=1`。

- `go test -race -count=1 -timeout=90s ./...` 全量通过，包含新增网关端到端测试；未发现数据竞争。
- 按 CONTRIBUTING.md 分项完成 `make verify` 对应检查：格式、`go vet ./...`、全量 race、`go build ./...` 和生成器 `-check` 均通过；`go mod verify` 通过、`go mod tidy -diff` 无差异，`git diff --check` 通过。
- `go test -race -count=1 -v -tags=integration -timeout=90s ./discovery ./storage/...` 全部通过，没有 skip。此前跳过的五项真实 etcd 用例以及 MySQL、Redis 各一项实例用例均实际执行。
- etcd 使用官方 Windows amd64 v3.7.2 发布包，按同版本 `SHA256SUMS` 校验（SHA256：`76989536a5f333642687fbd85603087c3deb4d316fd86fdfb63897b05f851cf4`）。每项测试启动独立进程、临时数据目录和随机回环端口，覆盖续租/租约丢失、删除恢复、实例所有权、Store 压缩与坏数据恢复，以及注册到连接池的完整链路。
- MySQL 8.4.11 和 Redis 8.2.10 使用 Podman 独立临时容器、随机回环端口；MySQL 验证连接池、SQL 和 GORM 读取，Redis 验证真实 PING。未复用现有开发服务，测试结束后移除临时容器及匿名测试卷。
- 协议 fuzz 再次运行 10 秒，通过 643,885 次执行，无失败输入；执行量不作为性能比较依据。

本次补充验证使用的服务版本如上，不替代 CI 中固定版本或多节点、持续负载、生产部署验证。

## numbox v0.4.1 升级验证（2026-10-18）

从 GitHub 远端核实最新 tag v0.4.1，对应提交 `f401ff44859935eb9b82ea98601ad68eea0427c4`。Orbit 从 v0.3.0 升级并同步 go.sum，无其他依赖版本变化或本地 replace。

- Windows amd64、Go 1.27.2、`CGO_ENABLED=1` 下，`go test -race -count=1 -timeout=90s ./...` 全量通过。
- 对实际依赖的 `github.com/NumberMan1/numbox/pkg/timewheel` 执行 race 测试，通过；不将其描述为 numbox 全库验证。
- Orbit 新增回归覆盖嵌套接口持有切片/映射，以及结构体/数组包含 NaN 的非法 key，确认 Schedule/Reset/Cancel 保持错误哨兵和拒绝语义；既有 nil key、空轮推进、时间边界和日志适配测试通过。
- 协议 fuzz 运行 10 秒，通过 2,567,764 次执行，无失败输入；执行量不作为性能比较依据。
- `go run ./examples/timers` 实际触发三次结构化 pulse 日志并正常退出。
- 格式、`go vet ./...`、`go build ./...`、生成器 `-check` 和 `go mod verify` 通过，`go mod tidy -diff` 无差异，`git diff --check` 通过。

本轮只升级时间轮依赖，未改动发现或存储实现，未重跑真实基础设施集成测试；上一轮结果与版本范围保留在上文。

## P1 故障处理与 P2 运行扩展验证（2026-10-18）

使用 Windows amd64、Go 1.27.2 与 `CGO_ENABLED=1`；未增加或升级第三方依赖。

- `go test -race -count=1 -timeout=90s ./...` 全量通过。新增行为覆盖心跳应答/迟到 Pong/失活断线、远端取消与截止时间、跨会话取消隔离、取消后仍占用执行名额、错误回包后继续请求、异步排空、独立排队等待计时、固定基数直方图、熔断代际与单探测恢复、排空状态更新不重建连接、分片顺序/并行/背压，以及配置策略拒绝、独立快照和并发版本一致性。
- 实际运行 gateway `-self-test`：在心跳就绪后向两台后端轮询发送四次请求，响应内容/序号正确，健康、JSON 指标和 Prometheus 导出端点可用，随后排空退出。该示例双方明确启用了请求元数据与取消 ID。
- 实际运行 runtime 示例：同一实体三个任务保持顺序，首个使用配置版本 1/limit 10，后两个使用版本 2/limit 20；禁止热更的监听地址变更被拒绝。
- `go test -race -count=1 -v -tags=integration -timeout=90s ./discovery` 通过；五项真实 etcd 用例全部执行、无 skip，使用此前校验过的官方 etcd v3.7.2 程序创建临时实例。未改动存储实现，本轮未重跑 MySQL/Redis 真实实例测试。
- 格式、`go vet ./...`、`go build ./...`、orbitgen `-check`、`go mod verify` 通过；`go mod tidy -diff` 无差异。
- 报文 fuzz 运行 10 秒，通过 3,497,919 次执行；新增元数据严格字段/版本回归与 request 包 race 通过，元数据 fuzz 运行 10 秒，通过 765,320 次执行，无失败输入。`make fuzz` 已包含两项；执行量不作为性能比较依据。
- `git diff --check` 与文档本地链接检查通过。

观测提供关联 ID 和适配接口，不是完整 span/OTLP 平台。未将上述检查视为持续负载、多节点故障切换、目标部署性能或远程 CI 验证。
