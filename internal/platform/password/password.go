// Package password 提供有并发限制的 Argon2id 密码处理。
package password

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/alexedwards/argon2id"
)

const memory uint32 = 19 * 1024
const iterations uint32 = 2
const maxPasswordBytes = 512
const maxEncodedHashBytes = 256

type Hasher struct {
	slots chan struct{}
	dummy string
}

func New(concurrency int) (*Hasher, error) {
	if concurrency < 1 {
		concurrency = 1
	}
	h := &Hasher{slots: make(chan struct{}, concurrency)}
	var err error
	h.dummy, err = h.Hash(context.Background(), "dummy password used only for timing")
	if err != nil {
		return nil, err
	}
	return h, nil
}

func Validate(value string) bool {
	n := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && n >= 15 && n <= 128 && len(value) <= maxPasswordBytes
}

func (h *Hasher) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case h.slots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-h.slots
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) Hash(ctx context.Context, value string) (string, error) {
	if len(value) > maxPasswordBytes {
		return "", fmt.Errorf("密码长度超出上限")
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-h.slots }()
	hash, err := argon2id.CreateHash(value, &argon2id.Params{
		Memory: memory, Iterations: iterations, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		return "", fmt.Errorf("密码哈希生成失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hash, nil
}

// Verify 先用库解析 PHC 并限制资源参数，再执行密码验证。
// 非法或超限哈希走 dummy 计算，但始终返回不匹配。
func (h *Hasher) Verify(ctx context.Context, hash, value string) (bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	if len(value) > maxPasswordBytes {
		return false, false, nil
	}
	var params *argon2id.Params
	valid := false
	if len(hash) <= maxEncodedHashBytes {
		var err error
		params, _, _, err = argon2id.DecodeHash(hash)
		valid = err == nil && allowedParams(params)
	}
	if !valid {
		hash = h.dummy
	}
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer func() { <-h.slots }()
	matched, err := argon2id.ComparePasswordAndHash(value, hash)
	if err != nil {
		return false, false, fmt.Errorf("密码哈希验证失败")
	}
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	matched = matched && valid
	return matched, matched && (params.Memory < memory || params.Iterations < iterations), nil
}

// allowedParams 仅维护服务的资源策略；PHC 格式、Base64 与常量时间比较交给库。
func allowedParams(p *argon2id.Params) bool {
	return p != nil && p.Memory >= 8 && p.Memory <= 64*1024 &&
		p.Iterations >= 1 && p.Iterations <= 4 && p.Parallelism == 1 &&
		p.SaltLength >= 16 && p.SaltLength <= 32 && p.KeyLength == 32
}
