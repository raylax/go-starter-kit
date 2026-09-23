// Package mail 定义邮件传输接口；具体发送方式由应用层装配。
package mail

import "context"

// Message 是与外部供应商无关的邮件内容，ID 可用于关联一次发送请求。
// HTML 是 HTML 格式正文，供应商适配器应按 text/html 投递。
// 收件地址、标题与正文均可能包含敏感内容，不得直接写入日志。
type Message struct {
	ID      string
	Kind    string
	To      string
	Subject string
	HTML    string
}

// Sender 返回 nil 表示实现已接受请求，不保证收件箱投递成功。
// 实现必须遵守上下文取消，不得把供应商响应中的敏感内容直接写入日志。
type Sender interface {
	Send(context.Context, Message) error
}
