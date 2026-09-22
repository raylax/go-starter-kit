package apperror_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/example/go-starter-kit/apperror"
)

func TestWrappingPreservesIdentityAndCause(t *testing.T) {
	project := apperror.New(apperror.NotFound, "project not found")
	task := apperror.New(apperror.NotFound, "task not found")
	err := fmt.Errorf("调用失败: %w", apperror.Wrap(project, context.DeadlineExceeded))
	if !errors.Is(err, project) || errors.Is(err, task) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("包装破坏了具体业务错误或底层原因")
	}
	var business *apperror.Error
	if !errors.As(err, &business) || business.Kind() != apperror.NotFound || business.Error() != "project not found" {
		t.Fatal("无法获得安全的业务错误分类和提示")
	}
	if apperror.Wrap(project, nil) != project || apperror.Wrap(nil, context.Canceled) != context.Canceled {
		t.Fatal("空参数处理改变了原错误")
	}
}
