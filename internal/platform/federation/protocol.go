package federation

// Protocol 是当前支持的第三方身份协议；提供商 ID 仍由配置扩展。
type Protocol string

const (
	ProtocolGitHub Protocol = "github"
)

func (p Protocol) Valid() bool { return p == ProtocolGitHub }
