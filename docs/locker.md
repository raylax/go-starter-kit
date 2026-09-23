# 公共 Locker

## 组织与使用

`internal/platform/locker` 定义 `Locker`、`Backend`、`Lease` 并实现公共 Runner；`postgres` 子包实现 PostgreSQL 后端。应用层创建专用锁连接池，注入后端、命名空间和运行参数，待所有任务退出后关闭连接池。

```go
ran, err := locks.TryRun(ctx, "report:generate:"+reportID, generateReport)
```

Locker 是独立公共能力，当前没有应用层默认装配或内置任务。公共层不暴露数据库连接、事务、手动解锁和续租。后端 SQL 是锁基础设施实现，不归入业务表的 sqlc 查询。

## 调用契约

下表为 `TryRun` 的返回值；`Run` 等待获取后执行，仅返回 error。

| 结果 | 语义 |
| --- | --- |
| `false, nil` | 锁竞争，回调未执行 |
| `false, err` | 获取、参数或取消错误，回调未执行 |
| `true, nil` | 回调与释放成功 |
| `true, err` | 回调已进入；业务、持锁或释放失败，不代表业务未提交 |

`TryRun` 不等待锁竞争，但获取连接和网络仍可能等待。`Run` 仅在竞争时做有抖动的指数退避，不重试基础设施错误或业务回调，不保证公平性。传入的 context 同时约束获取和执行。默认单次获取超时 5 秒、释放超时 5 秒，重试上限从 50 毫秒增长至 1 秒，可通过 `locker.Options` 调整。

回调在调用线程执行；panic 时完成清理后继续传播。取消或丢锁会取消回调的 context，Runner 等待回调退出，不能强制终止不遵守 context 的代码。回调返回后先停止并等待监控，再使用独立超时 context 解锁。业务错误、丢锁和释放错误通过错误链共同保留。释放失败不能撤销已经提交的业务。

同名锁不支持重入。使用回调传入的 context 再次获取同名锁会返回 `ErrReentrant`；自行替换为 Background 会丢失此检测，调用方必须遵守契约。后台子任务必须在回调返回前结束，不能把工作交给游离 goroutine 后提前解锁。

## 锁名称与标识

Namespace 为 1–64 字节的 ASCII 字母、数字、下划线、连字符或点；业务 key 最长 256 字节，额外允许冒号。不允许空值、空格、NUL 或秘密信息。完整名称为 `namespace:key`；Namespace 不含冒号，避免拼接歧义。

PostgreSQL 对完整名称做 SHA-256，前 64 位固定映射到两个 int32。极小概率的哈希冲突只造成无关任务相互等待。所有锁使用同一映射规则，不提供别名或特殊标识。

跨数据库、跨命名空间和跨后端不存在自动互斥；切换前应停止旧执行者。公共 Runner 本身不读取环境变量。

## PostgreSQL 后端

需要专用池，不能与业务 SQL 共享借出的连接，也不能使用 transaction pooling。获取、检测、释放始终使用同一条连接，连接不暴露给业务。获取结果不确定或释放失败时，从池中摘除并关闭连接；只有明确解锁成功的连接才能归还池。

默认每 2 秒探测一次会话，每次探测超时 2 秒。探测失败即按丢锁取消业务。正常停止监控时等待在途探测结束，避免通过取消探测意外关闭健康连接。探测只缩短故障发现时间，无法保证即时发现网络分区。

调用方显式创建连接池并设置 `locker.Options.Namespace`；Locker 不读取环境变量。所有协调同一任务的实例必须使用一致的后端和命名空间。

## 后端实现契约

`Backend.TryAcquire` 返回 `nil,nil` 表示竞争；返回错误之前清理不确定的持有权。成功返回的 Lease 由 Runner 独占。

`Lease.Watch` 阻塞监控直到取消或丢锁；取消必须有界退出，异常结束视为无法确认持有权。Runner 等待 Watch 返回后才调用 Release，避免共享连接并发操作。Release 必须核对所有权，不能解除其他持有者的锁。后端错误公开文本须脱敏，底层原因通过 `locker.Wrap` 保留。

未来 Redis 后端需原子获取带 TTL 的唯一持有者令牌，并原子比较令牌后续租和释放。无法及时确认持有权时取消回调，不允许无限重试续租并继续声称持有锁。

## 一致性范围

当前锁适用于可重试、幂等的清理和协调任务。PostgreSQL 锁连接与业务事务连接分离，丢锁后的旧回调可能尚未停止；context 是协作取消，不提供 exactly-once、严格单写入者或 fencing 保证。需要严格防止过期持有者写入时，必须增加资源端原子校验的 fencing 契约。Redis 主从切换及租约过期也需要单独评估，不能只替换配置就宣称一致性相同。

## 可观测性

提供 `locker.acquire.count`（acquired/busy/error）、`locker.acquire.duration`、`locker.wait.duration`、`locker.execution.duration`、`locker.lost.count`、`locker.release.error.count`。指标不包含完整 key、用户 ID 或持有者令牌。调用方可通过公共错误分类记录丢锁、锁不可用和释放失败，不应打印可能包含敏感信息的底层错误详情。
