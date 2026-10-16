# GitHub 配置

本目录存放仓库协作与自动验证配置，不属于运行时 Go 包。

- [workflows](workflows/README.md) 定义 push 和 pull request 的验证任务。
- 构建入口统一使用根目录 Makefile；工具链与依赖变更应同步检查 CI 配置。

验证范围见 [测试说明](../docs/testing.md)。
