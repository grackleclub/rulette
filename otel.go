package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

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

	// traces
	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, nil, fmt.Errorf("otel trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// metrics
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

	// logs
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
