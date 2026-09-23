// Package mailoutbox 处理持久化邮件的领取、重试和投递状态。
package mailoutbox

import "context"

// Message 只在发送调用期间使用，不得写入日志或审计。
type Message struct{ ID, Kind, To, Subject, HTML string }
type SendFunc func(context.Context, Message) error
