package observability

import (
	"context"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc/credentials/insecure"
)

func InitTracing(ctx context.Context, cfg config.Config, serviceName string) (func(context.Context) error, error) {
	if !cfg.TracesEnabled {
		return func(context.Context) error { return nil }, nil
	}
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()))
	}
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}
	provider := tracesdk.NewTracerProvider(tracesdk.WithBatcher(exporter), tracesdk.WithResource(res))
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}
