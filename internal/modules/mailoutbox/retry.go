package mailoutbox

import (
	"math/rand/v2"
)

// retryDelaySeconds 按领取次数退避，抖动避免故障恢复时集中重试。
func retryDelaySeconds(attempt int32) int64 {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	return int64(15*(1<<uint(attempt-1))) + int64(rand.IntN(10))
}
