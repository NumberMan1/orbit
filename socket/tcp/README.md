# socket/tcp

将 TCP/TLS 字节流适配为共享 Session，使用 [rpc](../../rpc/README.md) 帧边界。

- Server.Start 同步完成监听，后台接受连接；通过 Session/SessionCount 查询会话，Stop 关闭监听与会话并等待退出。
- Dial 返回尚未运行的 Session，调用方负责 Run 和 Close；NewConnector 包装自动重连和组件生命周期。
- 重连创建新会话，不重放断线前数据。使用 WaitSession 判断传输建立，应用认证另行处理。
- 通过 Options 设置队列、报文上限、读写超时、TCP 缓冲区及 NoDelay；网络选项设置失败不会继续使用该连接。
- TLS 配置与证书由调用方提供；监听使用跨平台标准库，不默认启用多进程端口复用。

参见 [会话约定](../README.md)、[服务端示例](../../examples/echo/README.md) 和 [客户端示例](../../examples/client/README.md)。
