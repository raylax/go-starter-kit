package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/example/go-starter-kit/internal/apperror"
)

const uniqueViolationSQLState = "23505"

// ErrorPolicy 声明本业务可识别的数据库错误；未声明的约束不会对外公开。
type ErrorPolicy struct {
	Resource          string
	NotFound          *apperror.Error
	UniqueConstraints map[string]*apperror.Error
}

func MapError(err error, policy ErrorPolicy) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) && policy.NotFound != nil {
		return apperror.Wrap(policy.NotFound, err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationSQLState {
		if business := policy.UniqueConstraints[pgErr.ConstraintName]; business != nil {
			return apperror.Wrap(business, err)
		}
	}
	return fmt.Errorf("%s storage: %w", policy.Resource, err)
}
