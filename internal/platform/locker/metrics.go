package locker

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// 指标只使用固定结果标签，不记录资源 ID、完整 key 或持有者令牌。
type metrics struct {
	attempts, lost, releaseErrors    metric.Int64Counter
	acquisitionTime, wait, execution metric.Float64Histogram
}

func newMetrics() (*metrics, error) {
	meter := otel.Meter("go-starter-kit/locker")
	m := new(metrics)
	var errs [6]error
	m.attempts, errs[0] = meter.Int64Counter("locker.acquire.count")
	m.lost, errs[1] = meter.Int64Counter("locker.lost.count")
	m.releaseErrors, errs[2] = meter.Int64Counter("locker.release.error.count")
	m.acquisitionTime, errs[3] = meter.Float64Histogram("locker.acquire.duration", metric.WithUnit("s"))
	m.wait, errs[4] = meter.Float64Histogram("locker.wait.duration", metric.WithUnit("s"))
	m.execution, errs[5] = meter.Float64Histogram("locker.execution.duration", metric.WithUnit("s"))
	return m, errors.Join(errs[:]...)
}
func (m *metrics) acquisition(ctx context.Context, outcome string, d time.Duration) {
	opts := metric.WithAttributes(attribute.String("outcome", outcome))
	m.attempts.Add(ctx, 1, opts)
	m.acquisitionTime.Record(ctx, d.Seconds(), opts)
}
