# codec

将应用消息与 `rpc.Frame` 相互转换，提供 JSON v2、现代 Protobuf 和可扩展的 Codec 接口。

- Registry 为实例级注册表，零值可用；按消息 ID、名称和 Go 类型查询，重复注册整体失败。
- 注册样本必须是结构体指针；Decode 为每条消息创建新实例，Encode 保留调用方传入的 Sequence。
- 注册表索引支持并发访问；编解码在锁外执行，自定义 Codec 必须支持并发使用。
- JSON 直接使用 `encoding/json/v2`；Protobuf 使用 `google.golang.org/protobuf`。消息定义与协议 ID 由应用维护。
- 此包负责序列化，帧长度校验交给 [rpc](../rpc/README.md)，网络处理与回复交给 [socket](../socket/README.md)。

参见 [JSON v2 行为](../docs/go127.md) 与 [消息接入](../docs/framework-extensions.md)。
