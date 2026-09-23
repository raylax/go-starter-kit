package account

import (
	"context"
	"crypto/sha256"
	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"time"
)

func (s *Service) limit(ctx context.Context, category, key string, max int32, window time.Duration) error {
	digest := sha256.Sum256([]byte(category + "\x00" + key))
	count, e := s.queries.RateLimit(ctx, sqlc.RateLimitParams{BucketKey: digest[:], WindowSeconds: int64(window.Seconds())})
	if e != nil {
		return apperror.Wrap(ErrUnavailable, e)
	}
	if count > max {
		return ErrRateLimited
	}
	return nil
}
func (s *Service) entryLimit(ctx context.Context, r Request, category, key string) error {
	if e := s.limit(ctx, category+".ip", r.ClientIP, 60, time.Minute); e != nil {
		return e
	}
	return s.limit(ctx, category+".subject", key, 10, 15*time.Minute)
}
