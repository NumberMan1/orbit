# 贡献指南

欢迎提交缺陷修复、文档、示例、测试和通用服务器能力改进。交流请遵守
[行为准则](CODE_OF_CONDUCT.md)；使用问题见 [SUPPORT.md](SUPPORT.md)，漏洞见 [SECURITY.md](SECURITY.md)。

## 开始之前

先搜索已有 Issue。新增公开接口、协议、依赖或生命周期语义变更，请说明具体场景、兼容性和方案。
Bug 报告应包含版本、Go 版本、运行环境及不依赖私有业务服务的最小复现。中文和英文均可。

## 本地开发

使用 **Go 1.27.2**，模块路径为 `github.com/NumberMan1/orbit`。JSON 使用标准库 `encoding/json/v2`，
无需实验开关。仓库约定见 [AGENTS.md](AGENTS.md)。

```sh
go mod download
make verify
make fuzz
git diff --check
```

`make verify` 检查精确工具链版本、格式、vet、竞态、构建及生成物一致性。没有 Make 的环境可执行：

```sh
gofmt -l .
go vet ./...
go test -race -count=1 -timeout=90s ./...
go build ./...
go run ./cmd/orbitgen -in examples/gateway/protocol/messages.json -out examples/gateway/protocol/bindings_gen.go -check
go test ./rpc -run='^$' -fuzz=FuzzRead -fuzztime=10s
go test ./request -run='^$' -fuzz=FuzzMetadata -fuzztime=10s
```

`gofmt -l .` 应没有输出。竞态检测需要平台支持并安装可用的 C 编译器。
普通测试不依赖外部服务，但网络测试需要绑定回环随机端口。
集成测试使用隔离的 etcd、MySQL、Redis 测试实例，配置方法见 [测试说明](docs/testing.md)；
缺少服务导致的 skip 不应报告为集成验证通过。

## 修改约定

- 保持框架通用，不引入私有业务模块、用户数据、内置凭据或业务协议生成物。
- 使用 `gofmt`，为导出接口补充并发、nil、资源归属、错误及关闭行为的文档。
- 组件只借用调用方日志；停止所有使用者后由应用关闭 core。
- 时间轮算法由 `github.com/NumberMan1/numbox/pkg/timewheel` 维护，Orbit 直接导入上游包，仅在 `logging` 提供日志适配，不保留 util 转发入口。
- 改动协议、回调、并发或生命周期时，补充调用方可见行为的回归测试，并同步更新示例与文档。
- 依赖调整运行 `go mod tidy`，提交 `go.mod` 和 `go.sum`；使用发布版本，不提交本地路径 replace。
- 面向用户的变更记入 [CHANGELOG.md](CHANGELOG.md)，破坏性变更说明迁移路径。
- 保留第三方许可和来源，不提交 IDE 配置、编译输出、日志、测试产物或真实配置。

## Pull Request

从 fork 创建分支，向仓库默认分支发起 PR，一次 PR 聚焦一个问题。
描述最终行为、解决的问题、兼容性及实际验证结果；未运行或失败的检查须说明原因。
维护者评审通过后合并。

提交贡献表示你有权提供这些内容，并同意原创贡献采用项目 [MIT 许可证](LICENSE)。
第三方内容保留自身许可，历史来源核对见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
