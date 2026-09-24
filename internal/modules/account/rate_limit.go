package account

import (
	"context"
	"crypto/sha256"
	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"time"
)

const (
	ipRequestLimit      = 60
	ipRateWindow        = time.Minute
	subjectRequestLimit = 10
	subjectRateWindow   = 15 * time.Minute
)

func (s *Service) limit(ctx context.Context, category, key string, max int32, window time.Duration) error {
	digest := sha256.Sum256([]byte(category + "\x00" + key))
	bucket, e := s.queries.RateLimit(ctx, sqlc.RateLimitParams{BucketKey: digest[:], WindowSeconds: int64(window.Seconds())})
	if e != nil {
		return apperror.Wrap(ErrUnavailable, e)
	}
	if bucket.Count > max {
		return apperror.WithRetryAfter(ErrRateLimited, time.Duration(bucket.RetryAfterSeconds)*time.Second)
	}
	return nil
}
func (s *Service) entryLimit(ctx context.Context, r Request, category, key string) error {
	if e := s.limit(ctx, category+".ip", r.ClientIP, ipRequestLimit, ipRateWindow); e != nil {
		return e
	}
	return s.limit(ctx, category+".subject", key, subjectRequestLimit, subjectRateWindow)
}
