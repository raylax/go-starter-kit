package apperror

import (
	"errors"
	"time"
)

type retryError struct {
	cause error
	delay time.Duration
}

func (e *retryError) Error() string { return e.cause.Error() }
func (e *retryError) Unwrap() error { return e.cause }

// WithRetryAfter 将业务决定的等待时间附加到错误链，不依赖 HTTP。
func WithRetryAfter(err error, delay time.Duration) error {
	if err == nil {
		return nil
	}
	if delay < time.Second {
		delay = time.Second
	}
	return &retryError{cause: err, delay: delay}
}

func RetryAfter(err error) (time.Duration, bool) {
	var retry *retryError
	if !errors.As(err, &retry) {
		return 0, false
	}
	return retry.delay, true
}
