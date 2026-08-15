// Package logger provides a slog-based JSON logger configured from a level
// string, optionally fanning records out to an OTLP log collector.
package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// New returns a JSON slog.Logger writing to stderr at the given level
// ("debug", "info", "warn", "error"; anything else falls back to info).
func New(level string) *slog.Logger {
	return slog.New(stderrHandler(level))
}

// Options configures a logger that writes JSON to stderr and, when
// OTLPEndpoint is set, also ships records to an OTLP/gRPC collector.
type Options struct {
	Level        string // "debug", "info", "warn", "error"; default info
	Service      string // service.name resource attribute, e.g. "tax-deeds-api"
	Environment  string // deployment.environment resource attribute; default "dev"
	OTLPEndpoint string // e.g. "http://localhost:4317"; empty disables OTLP
}

// ShutdownFunc flushes buffered OTLP records; a no-op when OTLP is disabled.
type ShutdownFunc func(context.Context) error

// NewWithOTLP returns a logger per Options plus the shutdown func that must
// be called before exit so batched records reach the collector.
func NewWithOTLP(ctx context.Context, opts Options) (*slog.Logger, ShutdownFunc, error) {
	stderr := stderrHandler(opts.Level)
	if opts.OTLPEndpoint == "" {
		return slog.New(stderr), func(context.Context) error { return nil }, nil
	}

	env := opts.Environment
	if env == "" {
		env = "dev"
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(opts.Service),
		semconv.DeploymentEnvironment(env),
	))
	if err != nil {
		return nil, nil, fmt.Errorf("otel resource: %w", err)
	}

	// An http:// endpoint URL implies plaintext gRPC, matching the collector's
	// insecure OTLP receiver. The exporter does not dial eagerly, so an
	// unreachable collector never blocks startup.
	exporter, err := otlploggrpc.New(ctx, otlploggrpc.WithEndpointURL(opts.OTLPEndpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("otlp log exporter: %w", err)
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter,
			sdklog.WithMaxQueueSize(2048),
			sdklog.WithExportMaxBatchSize(512),
			sdklog.WithExportInterval(time.Second),
		)),
	)
	// otelslog reports Enabled for every level; the wrapper keeps the OTLP
	// stream at the same level as stderr.
	otel := levelHandler{
		min:     parseLevel(opts.Level),
		Handler: otelslog.NewHandler(opts.Service, otelslog.WithLoggerProvider(provider)),
	}
	return slog.New(slog.NewMultiHandler(stderr, otel)), provider.Shutdown, nil
}

func stderrHandler(level string) slog.Handler {
	return slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(level)})
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// levelHandler filters records below min before delegating.
type levelHandler struct {
	min slog.Level
	slog.Handler
}

func (h levelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.Handler.Enabled(ctx, l)
}

func (h levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelHandler{h.min, h.Handler.WithAttrs(attrs)}
}

func (h levelHandler) WithGroup(name string) slog.Handler {
	return levelHandler{h.min, h.Handler.WithGroup(name)}
}
