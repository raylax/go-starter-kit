package identity

import "github.com/google/uuid"

// NewID 生成不透明的字符串标识；生成策略集中在此处，调用方不依赖格式。
// 返回值须满足 ValidateSubject 的标识约束。
// 当前使用 UUID v4，替换实现不改写已有记录的标识。
func NewID() string { return uuid.NewString() }
