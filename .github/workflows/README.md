# 自动验证流程

[ci.yml](ci.yml) 在 push、pull request 或手动触发时运行，固定 Go 1.27.2 与 GOTOOLCHAIN=local。
同一 ref 的新运行会取消旧运行，每个任务最多运行 20 分钟。

- verify：格式、vet、全量 race、构建与协议 fuzz；另交叉构建 Windows amd64、macOS arm64。
- infrastructure：准备 MySQL 8.4、Redis 7.4 和 etcd 3.6.7，运行集成测试；etcd 测试自行创建临时实例。
- 流程使用只读仓库权限，测试凭据只用于临时 CI 服务；不承担发布或部署。

本机执行入口及外部服务参数见 [测试说明](../../docs/testing.md)。
