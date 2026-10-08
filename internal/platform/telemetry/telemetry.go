// Package telemetry wires OpenTelemetry traces, metrics and logs.
//
// Exporter settings come from the standard OTEL_* environment variables
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME, OTEL_TRACES_SAMPLER, ...),
// which the SDK reads on its own. When disabled, the global no-op providers
// stay in place so instrumentation calls cost almost nothing.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/z-alamsyah/codebase-go/internal/config"
)

// Telemetry holds the providers created at startup.
type Telemetry struct {
	// LogHandler forwards slog records to the OTel log pipeline. Nil when disabled.
	LogHandler slog.Handler
	shutdowns  []func(context.Context) error
}

// Setup initializes OpenTelemetry when cfg.Enabled is true.
func Setup(ctx context.Context, app config.App, cfg config.Otel) (*Telemetry, error) {
	t := &Telemetry{}
	if !cfg.Enabled {
		return t, nil
	}

	// Later detectors win, so OTEL_SERVICE_NAME / OTEL_RESOURCE_ATTRIBUTES
	// override the defaults taken from APP_* variables.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(app.Name),
			semconv.ServiceVersion(app.Version),
			semconv.DeploymentEnvironmentName(app.Env),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	traceExp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otel trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.shutdowns = append(t.shutdowns, tp.Shutdown)

	metricExp, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otel metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)), sdkmetric.WithResource(res))
	otel.SetMeterProvider(mp)
	t.shutdowns = append(t.shutdowns, mp.Shutdown)

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nil, fmt.Errorf("otel runtime metrics: %w", err)
	}

	logExp, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otel log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)), sdklog.WithResource(res))
	otel.SetLoggerProvider(lp)
	t.shutdowns = append(t.shutdowns, lp.Shutdown)
	t.LogHandler = otelslog.NewHandler(app.Name, otelslog.WithLoggerProvider(lp))

	// Export failures (collector down, etc.) must never crash the app.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Warn("opentelemetry export error", slog.Any("error", err))
	}))

	return t, nil
}

// Shutdown flushes pending telemetry. Safe to call when disabled.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	for i := len(t.shutdowns) - 1; i >= 0; i-- {
		errs = append(errs, t.shutdowns[i](ctx))
	}
	return errors.Join(errs...)
}
