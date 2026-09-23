package locker

import "context"

// Backend 仅供后端适配与 Runner 装配。必须支持并发获取。
// 竞争返回 nil,nil；出错时必须清理可能已经获取的锁，不能遗留不明持有权。
// key 已包含命名空间。实现必须遵循 context，不自动重试业务。
type Backend interface {
	TryAcquire(ctx context.Context, key string) (Lease, error)
}

// Lease 由 Runner 独占。Watch 结束后才调用 Release，两者不并发。
// Watch 阻塞监控，无法确认持有权时返回错误；ctx 取消时应有界退出。
// Release 必须核对持有权，不能释放其他持有者的锁。
type Lease interface {
	Watch(context.Context) error
	Release(context.Context) error
}
