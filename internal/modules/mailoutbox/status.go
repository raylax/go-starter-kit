package mailoutbox

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
