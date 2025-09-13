package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"

	appconfig "github.com/traffictacos/inventory-api/internal/config"
)

var tracer trace.Tracer

// InitTracer initializes OpenTelemetry tracer
func InitTracer(cfg *appconfig.Config) error {
	ctx := context.Background()

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Observability.OTLPEndpoint),
		otlptracegrpc.WithInsecure(), // Use secure connection in production
	)
	if err != nil {
		return fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.Observability.ServiceName),
			semconv.ServiceVersionKey.String(cfg.Observability.ServiceVersion),
			semconv.ServiceNamespaceKey.String("traffic-tacos"),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Adjust sampling in production
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer = tp.Tracer(cfg.Observability.ServiceName)

	return nil
}

// GetTracer returns the global tracer
func GetTracer() trace.Tracer {
	return tracer
}

// StartSpan starts a new span with the given name
func StartSpan(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, spanName, opts...)
}

// AddSpanAttributes adds attributes to the current span
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span != nil {
		span.SetAttributes(attrs...)
	}
}

// RecordError records an error on the current span
func RecordError(ctx context.Context, err error, description string) {
	span := trace.SpanFromContext(ctx)
	if span != nil {
		span.RecordError(err, trace.WithAttributes(
			attribute.String("error.description", description),
		))
		span.SetStatus(codes.Error, description)
	}
}

// EndSpan ends the current span
func EndSpan(ctx context.Context) {
	span := trace.SpanFromContext(ctx)
	if span != nil {
		span.End()
	}
}

// TraceMethod is a helper function to trace method execution
func TraceMethod(ctx context.Context, methodName string, fn func(context.Context) error, attrs ...attribute.KeyValue) error {
	ctx, span := StartSpan(ctx, methodName,
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer EndSpan(ctx)

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(
		attribute.String("method.name", methodName),
		attribute.Int64("method.duration_ms", duration.Milliseconds()),
	)

	if err != nil {
		RecordError(ctx, err, fmt.Sprintf("method %s failed", methodName))
		return err
	}

	return nil
}
