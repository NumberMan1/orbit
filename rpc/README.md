# rpc

负责二进制帧封装，保留旧 RO 明文帧布局；包名不代表已实现远程函数调用。

- 消息头为 10 字节大端序：payload 长度 uint16、ID uint16、Sequence uint32、flags uint16。长度不含消息头。
- flags 必须为 0；应用传入的 payload 上限须在 1..65535，空 payload 合法。加密使用传输层 TLS。
- Read 使用完整读取，拒绝截断、超限和非法 flags；Write 补齐短写并拒绝零进展。
- Sequence 原样透传；请求匹配、超时、ACK、重试与消息语义由应用定义。
- 此包无连接和共享状态；同一流的并发读写排序由 [socket.Session](../socket/README.md) 管理。

消息内容使用 [codec](../codec/README.md)，协议迁移约束见 [迁移说明](../docs/migration.md)。
