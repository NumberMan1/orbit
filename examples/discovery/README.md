# discovery 示例

将 etcd 服务目录的 Store 快照作为只读 HTTP 接口暴露，演示就绪与缓存的区别。

先准备 etcd，在仓库根目录运行：

```sh
go run ./examples/discovery -endpoints http://127.0.0.1:2379 -prefix /orbit/example/services -listen 127.0.0.1:9010
```

- `GET /readyz` 返回目录同步状态；`GET /services` 输出快照。Store 未就绪时两者均返回 503。
- Store 先于 HTTP 启动，退出时先停 HTTP 再停 Store，最后由入口关闭其借用的 etcd 客户端。
- Observer 输出同步事件；本示例只读取目录，不注册服务，也不连接目录中的节点。
- 示例连接不包含认证/TLS 参数，实际应用需显式配置 etcd 客户端。

目录数据格式、演示写入与故障恢复见 [Store 文档](../../docs/discovery-store.md)。
