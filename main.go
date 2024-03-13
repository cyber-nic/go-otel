// Package main is the main package for the API.
package main

import (
	"context"
	"fmt"
	"net/http"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/riandyrn/otelchi"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
)

var fooCounter = prom.NewCounter(prom.CounterOpts{
	Name: "api_foo_requests_total",
	Help: "Total number of requests to the /foo endpoint.",
})

func init() {
	// Register the counter with Prometheus's default registry.
	prom.MustRegister(fooCounter)
}

func main() {
	// Create a context with a cancelletion
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// initialize trace provider
	svcName := "go-otel"
	initTracer(ctx, svcName)

	// The exporter embeds a default OpenTelemetry Reader and
	// implements prometheus.Collector, allowing it to be used as
	exporter, err := prometheus.New()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create prometheus exporter")
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	// Start the prometheus HTTP server and pass the exporter Collector to it
	go serveMetrics()

	router := chi.NewRouter()

	// router.Use(httplog.RequestLogger(l))
	router.Use(middleware.Heartbeat("/ping"))
	router.Use(middleware.Recoverer)
	router.Use(render.SetContentType(render.ContentTypeJSON))
	router.Use(middleware.RequestID)
	router.Use(otelchi.Middleware(svcName))

	router.Get("/foo", func(w http.ResponseWriter, r *http.Request) {
		// Increment the counter for each request to /foo
		fooCounter.Inc()

		w.Write([]byte("bar"))
		log.Info().Caller().Str("foo", "bar").Msg("get")
	})

	addr := fmt.Sprintf("0.0.0.0:%d", 8080)
	log.Info().Caller().Msgf("listening: %s", addr)
	http.ListenAndServe(addr, router)
}

func initTracer(ctx context.Context, svcName string) {
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint("localhost:4317"),
		otlptracegrpc.WithInsecure(), // Use WithInsecure for non-TLS, or configure TLS with appropriate options.
	)
	// Configure the OTLP exporter to send traces to your Otel Collector.
	exporter, err := otlptrace.New(ctx, client)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create exporter")
	}

	// Create a new trace provider with a batch span processor and the otlp exporter.
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(svcName),
		)),
	)

	// Register the trace provider globally.
	otel.SetTracerProvider(tp)
}

func serveMetrics() {
	log.Info().Caller().Msgf("metrics: %s", "localhost:2222/metrics")
	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(":2222", nil) //nolint:gosec // Ignoring G114: Use of net/http serve function that has no support for setting timeouts.
	if err != nil {
		fmt.Printf("error serving http: %v", err)
		return
	}
}
