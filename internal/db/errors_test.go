package db_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db"
)

func TestStorageErrorPolicy(t *testing.T) {
	missing := apperror.New(apperror.NotFound, "not found")
	conflict := apperror.New(apperror.Conflict, "name already exists")
	policy := db.ErrorPolicy{Resource: "project", NotFound: missing, UniqueConstraints: map[string]*apperror.Error{"projects_owner_name_key": conflict}}
	for _, tc := range []struct {
		name  string
		cause error
		want  *apperror.Error
	}{
		{"没有记录", pgx.ErrNoRows, missing},
		{"已声明的唯一约束", &pgconn.PgError{Code: "23505", ConstraintName: "projects_owner_name_key"}, conflict},
		{"未声明的唯一约束", &pgconn.PgError{Code: "23505", ConstraintName: "other_key"}, nil},
		{"其他约束类型", &pgconn.PgError{Code: "23514", ConstraintName: "projects_owner_name_key"}, nil},
		{"请求取消", context.Canceled, nil},
		{"请求超时", context.DeadlineExceeded, nil},
		{"未知故障", errors.New("private database failure"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("query: %w", tc.cause)
			err := db.MapError(wrapped, policy)
			if !errors.Is(err, tc.cause) {
				t.Fatal("丢失底层原因")
			}
			var business *apperror.Error
			classified := errors.As(err, &business)
			if tc.want == nil && classified {
				t.Fatal("内部异常被错误公开为业务错误")
			}
			if tc.want != nil && (!classified || !errors.Is(err, tc.want)) {
				t.Fatal("业务分类或错误身份不正确")
			}
			if (errors.Is(tc.cause, context.Canceled) || errors.Is(tc.cause, context.DeadlineExceeded)) && err != wrapped {
				t.Fatal("取消错误应原样传递")
			}
		})
	}
	if db.MapError(nil, policy) != nil {
		t.Fatal("成功操作被转换为错误")
	}
	var business *apperror.Error
	if errors.As(db.MapError(pgx.ErrNoRows, db.ErrorPolicy{}), &business) {
		t.Fatal("无映射策略时不应推断业务错误")
	}
}
