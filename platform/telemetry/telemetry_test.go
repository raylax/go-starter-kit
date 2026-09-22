package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestOTLPExportsTracesAndMetrics(t *testing.T) {
	var traces, metrics atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil || len(data) == 0 {
			t.Error("empty OTLP payload")
		}
		switch r.URL.Path {
		case "/v1/traces":
			traces.Add(1)
		case "/v1/metrics":
			metrics.Add(1)
		default:
			t.Errorf("unexpected OTLP path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", collector.URL+"/v1/metrics")
	previousTraces, previousMetrics, previousPropagation := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()
	defer func() {
		otel.SetTracerProvider(previousTraces)
		otel.SetMeterProvider(previousMetrics)
		otel.SetTextMapPropagator(previousPropagation)
	}()
	shutdown, err := Setup(t.Context(), "telemetry-test", true)
	if err != nil {
		t.Fatal(err)
	}
	// 使用明确采样的远程父跨度，避免 10% 采样率使测试结果随机变化。
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled, Remote: true})
	ctx := trace.ContextWithRemoteSpanContext(t.Context(), parent)
	_, span := otel.Tracer("test").Start(ctx, "operation")
	span.End()
	counter, err := otel.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(ctx, 1)
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(flushCtx); err != nil {
		t.Fatal(err)
	}
	if traces.Load() == 0 || metrics.Load() == 0 {
		t.Fatalf("traces=%d metrics=%d", traces.Load(), metrics.Load())
	}
}
