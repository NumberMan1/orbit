# logging

为标准 `log/slog` 提供可选的按文件大小轮转输出，不引入全局 logger。

- `NewRotatingWriter` 返回可传给 `slog.NewJSONHandler` 或 TextHandler 的 writer，串行化并发写入。
- 超过 MaxBytes 前轮转，最多保留 Backups 个备份；单条超大记录返回 ErrRecordTooLarge，不拆分记录。
- 调用方独占日志文件和备份路径，负责 Close；需要主动刷盘时调用 Sync。
- 新文件以 0600、目录以 0700 创建；既有文件权限由调用方管理。不得与其他进程或外部轮转器共写。
- I/O 错误由 writer 返回；slog 的日志调用本身不返回写入错误，需要告警时由应用封装 Handler/输出。轮转不保证跨文件事务或崩溃原子性。

参见 [日志接入与边界](../docs/framework-extensions.md)。
