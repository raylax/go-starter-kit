// Package mailoutbox 处理持久化邮件的领取、重试和投递状态。
package mailoutbox

import "context"

// Message 只在发送调用期间使用，不得写入日志或审计。
type Message struct{ ID, Kind, To, Subject, HTML string }
type SendFunc func(context.Context, Message) error

// Status 描述队列投递生命周期，状态转换由原子 SQL 执行。
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusSent       Status = "sent"
	StatusFailed     Status = "failed"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusProcessing, StatusSent, StatusFailed:
		return true
	default:
		return false
	}
}

func (s Status) Terminal() bool { return s == StatusSent || s == StatusFailed }
