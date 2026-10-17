# orbitgen

根据显式 JSON manifest 生成 `RegisterMessages`、可选的 `Handlers` 与 `RegisterHandlers`。
生成结果使用 Orbit 的 codec.Registry、socket.Router 和 MessageEvent。

```json
{
  "package": "protocol",
  "messages": [
    {"id": 1, "type": "Echo", "codec": "json", "handler": "Echo"},
    {"id": 2, "type": "EchoReply", "codec": "json"}
  ]
}
```

```sh
go run ./cmd/orbitgen -in examples/gateway/protocol/messages.json -out examples/gateway/protocol/bindings_gen.go
go run ./cmd/orbitgen -in examples/gateway/protocol/messages.json -out examples/gateway/protocol/bindings_gen.go -check
```

- 使用已有 Go struct 类型；codec 为 json 或 protobuf。Protobuf 类型先用 protoc 生成，本工具不生成模型或修改线协议。
- 外部类型可配置 `"imports":{"pb":"example.com/project/protocol"}`，type 写为 `pb.Request`；别名不得覆盖工具自身导入名。
- ID 必须在 1..65535，ID/type 不重复；handler 可省略，提供时必须是唯一的导出标识符。
- manifest 最大 1 MiB，拒绝未知字段、重复 JSON 字段、未使用导入和非法标识符。
- 输出按消息 ID 排序并使用 gofmt；`-check` 只检查文件一致性，不写文件，可用于 CI。
- 默认只覆盖包含工具生成标记的文件，拒绝覆盖手写文件。注册应使用新 Registry/Router；遇错返回，不保证多个注册项整体回滚。

实际输入和生成结果见 [示例协议](../../examples/gateway/protocol/messages.go)。
