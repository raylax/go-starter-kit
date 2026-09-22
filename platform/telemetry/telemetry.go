package telemetry

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

func Setup(ctx context.Context, serviceName string, enabled bool) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !enabled {
		return func(context.Context) error { return nil }, nil
	}
	res, err := resource.New(ctx, resource.WithFromEnv(), resource.WithAttributes(attribute.String("service.name", serviceName)))
	if err != nil {
		return nil, err
	}
	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		_ = traceExporter.Shutdown(ctx)
		return nil, err
	}
	traces := trace.NewTracerProvider(trace.WithResource(res), trace.WithBatcher(traceExporter), trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(0.1))))
	metrics := metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(metric.NewPeriodicReader(metricExporter)))
	otel.SetTracerProvider(traces)
	otel.SetMeterProvider(metrics)
	return func(ctx context.Context) error { return errors.Join(traces.Shutdown(ctx), metrics.Shutdown(ctx)) }, nil
}
