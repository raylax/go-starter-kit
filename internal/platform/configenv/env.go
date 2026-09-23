// Package configenv 将环境变量解析为配置结构体，统一处理空值和错误脱敏。
package configenv

import (
	"errors"
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Parse 仅使用注入的 lookup，不回退读取进程环境。
// 声明非空默认值的字段，显式空字符串视为配置错误，只有未设置时使用默认值。
func Parse[T any](lookup func(string) (string, bool)) (T, error) {
	var cfg T
	if lookup == nil {
		return cfg, fmt.Errorf("环境变量读取函数不能为空")
	}
	fields, err := env.GetFieldParams(&cfg)
	if err != nil {
		return cfg, fmt.Errorf("配置结构定义无效")
	}
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		value, set := lookup(field.Key)
		if !set {
			continue
		}
		if value == "" && field.HasDefaultValue && field.DefaultValue != "" {
			return cfg, fmt.Errorf("%s 不能为空", field.Key)
		}
		values[field.Key] = value
	}
	if err := env.ParseWithOptions(&cfg, env.Options{Environment: values}); err != nil {
		return cfg, sanitize(err)
	}
	return cfg, nil
}

// 库的类型转换错误可能包含原始值，不能直接回显或保留到公开错误链。
func sanitize(err error) error {
	var aggregate env.AggregateError
	if errors.As(err, &aggregate) {
		safe := make([]error, 0, len(aggregate.Errors))
		for _, item := range aggregate.Errors {
			safe = append(safe, sanitize(item))
		}
		return errors.Join(safe...)
	}
	var parse env.ParseError
	if errors.As(err, &parse) {
		return fmt.Errorf("%s 格式无效", parse.Name)
	}
	var missing env.VarIsNotSetError
	if errors.As(err, &missing) {
		return fmt.Errorf("%s 未设置", missing.Key)
	}
	var empty env.EmptyVarError
	if errors.As(err, &empty) {
		return fmt.Errorf("%s 不能为空", empty.Key)
	}
	return fmt.Errorf("环境配置解析失败")
}
