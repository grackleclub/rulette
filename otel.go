package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

var (
	attrGameID     = attribute.Key("game.id")
	attrPlayerID   = attribute.Key("game.player_id")
	attrAction     = attribute.Key("game.action")
	attrTopic      = attribute.Key("game.topic")
	attrStateID    = attribute.Key("game.state_id")
	attrCallerName = attribute.Key("game.caller_name")
)

var (
	cacheHits   metric.Int64Counter
	cacheMisses metric.Int64Counter
)

// initMetrics registers cache instruments on the global meter provider.
// Safe to call when OTEL is disabled — the noop provider returns
// noop instruments. Must be called after the cache var exists.
func initMetrics(cache *sync.Map) error {
	m := otel.Meter("rulette")
	var err error
	cacheHits, err = m.Int64Counter("cache.hits",
		metric.WithDescription("Game state cache hits"),
	)
	if err != nil {
		return fmt.Errorf("cache.hits counter: %w", err)
	}
	cacheMisses, err = m.Int64Counter("cache.misses",
		metric.WithDescription("Game state cache misses"),
	)
	if err != nil {
		return fmt.Errorf("cache.misses counter: %w", err)
	}
	_, err = m.Int64ObservableGauge("cache.size",
		metric.WithDescription("Number of entries in the game state cache"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			var n int64
			cache.Range(func(_, _ any) bool {
				n++
				return true
			})
			o.Observe(n)
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("cache.size gauge: %w", err)
	}
	return nil
}

func otelResource(ctx context.Context) (*resource.Resource, error) {
	hostname, _ := os.Hostname()
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("rulette"),
			semconv.ServiceVersion(version),
			semconv.HostName(hostname),
		),
	)
}

// initOtel configures OpenTelemetry trace, metric, and log providers
// using OTLP/HTTP exporters. Returns a noop shutdown and nil handler
// when OTEL_EXPORTER_OTLP_ENDPOINT is unset. When set, the standard
// OTEL_EXPORTER_OTLP_* env vars (HEADERS, CERTIFICATE, etc.) are
// passed through to the OTLP exporters. The returned slog.Handler
// bridges log records to the OTel log provider via otelslog.
func initOtel(
	ctx context.Context,
) (shutdown func(context.Context) error, logHandler slog.Handler, err error) {
	noop := func(context.Context) error { return nil }

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return noop, nil, nil
	}

	res, err := otelResource(ctx)
	if err != nil {
		return noop, nil, fmt.Errorf("otel resource: %w", err)
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, nil, fmt.Errorf("otel trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return noop, nil, fmt.Errorf("otel metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(metricExp),
		),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	logExp, err := otlploghttp.New(ctx)
	if err != nil {
		return noop, nil, fmt.Errorf("otel log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)
	handler := otelslog.NewHandler("rulette",
		otelslog.WithLoggerProvider(lp),
	)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown = func(ctx context.Context) error {
		var errs []error
		if e := lp.Shutdown(ctx); e != nil {
			errs = append(errs, e)
		}
		if e := mp.Shutdown(ctx); e != nil {
			errs = append(errs, e)
		}
		if e := tp.Shutdown(ctx); e != nil {
			errs = append(errs, e)
		}
		if len(errs) > 0 {
			return fmt.Errorf("otel shutdown: %v", errs)
		}
		return nil
	}

	return shutdown, handler, nil
}
