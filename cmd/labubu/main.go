package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/labubu/labubu/internal/alerting"
	"github.com/labubu/labubu/internal/api"
	ilog "github.com/labubu/labubu/internal/log"
	"github.com/labubu/labubu/internal/metrics"
	"github.com/labubu/labubu/internal/pipeline"
	"github.com/labubu/labubu/internal/receiver"
	"github.com/labubu/labubu/internal/storage"
)

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	os.Exit(run())
}

// run dispatches subcommands and returns an exit code.
// Separated from main() so tests can call it without os.Exit.
func run() int {
	if len(os.Args) < 2 {
		printUsage()
		return 1
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
		return 0
	case "version":
		fmt.Printf("labubu %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		return 0
	case "help":
		printUsageTo(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		return 1
	}
}

func printUsage() {
	printUsageTo(os.Stderr)
}

func printUsageTo(w *os.File) {
	fmt.Fprintf(w, `Usage: labubu <command> [options]

Commands:
  serve     Start the Labubu server (OTLP receiver + API + UI)
  version   Print version information
  help      Show this help message

Run "labubu serve --help" for serve options.
`)
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)

	port := fs.Int("port", 8080, "API and UI listen port")
	dataDir := fs.String("data-dir", "", "data directory (empty = in-memory)")
	bufferSize := fs.Int("buffer-size", 1000, "pipeline buffer capacity")
	flushInterval := fs.Duration("flush-interval", 200*time.Millisecond, "pipeline flush interval")

	metricsEnabled := fs.Bool("metrics-enabled", true, "enable/disable metrics ingestion")
	metricsDataDir := fs.String("metrics-data-dir", "", "tstorage data directory (empty = pure memory)")

	logLevel := fs.String("log-level", "info", "log level: debug, info, warn, error")
	configPath := fs.String("config", "labubu.yaml", "config file path")

	fs.Parse(args)

	// Load YAML config.
	cfg := storage.LoadConfig(*configPath)

	apiAddr := fmt.Sprintf("0.0.0.0:%d", *port)

	// Check port availability before starting.
	if ln, err := net.Listen("tcp", apiAddr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: port %d is already in use.\nTry: labubu serve --port %d\n", *port, *port+1)
		os.Exit(1)
	} else {
		ln.Close()
	}

	// Set log level.
	lvl, err := ilog.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}
	ilog.SetLevel(lvl)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Print startup banner.
	fmt.Printf("Labubu v%s starting...\n", Version)
	fmt.Printf("  OTLP gRPC:      http://localhost:4317\n")
	fmt.Printf("  OTLP HTTP:      http://localhost:4318\n")
	fmt.Printf("  API & UI:       http://localhost:%d\n", *port)
	if *dataDir == "" {
		fmt.Printf("  Storage:        in-memory (data lost on exit)\n")
	} else {
		fmt.Printf("  Storage:        %s\n", *dataDir)
	}
	fmt.Printf("  Trace retention:  max_age=%s, max_count=%d, cleanup=%s\n",
		cfg.Trace.Retention.MaxAge, cfg.Trace.Retention.MaxCount, cfg.Trace.Retention.CleanupInterval)
	fmt.Printf("  Metric retention: max_age=%s\n", cfg.Metric.Retention.MaxAge)
	fmt.Println()

	// Initialize storage (in-memory for non-CGO builds).
	store, err := storage.NewChDBStore(*dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	// Start retention cleanup goroutine.
	retentionCtx, retentionCancel := context.WithCancel(context.Background())
	defer retentionCancel()
	go runRetentionCleanup(retentionCtx, store, cfg.Trace.Retention)

	// Initialize metrics store (if enabled).
	var metricStore metrics.Store
	if *metricsEnabled {
		ms, err := metrics.NewTStorageStore(metrics.TStorageConfig{
			DataDir:   *metricsDataDir,
			Retention: cfg.Metric.Retention.MaxAge,
		})
		if err != nil {
			log.Fatalf("Failed to initialize metrics store: %v", err)
		}
		defer ms.Close()
		metricStore = ms
	}

	// Initialize pipeline.
	pipe := pipeline.New(store, *bufferSize, *flushInterval)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pipe.Shutdown(ctx); err != nil {
			log.Printf("Pipeline shutdown error: %v", err)
		}
	}()

	// Initialize OTLP receiver.
	recv := receiver.New(pipe, metricStore, store)
	if err := recv.Start(); err != nil {
		log.Fatalf("Failed to start OTLP receiver: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := recv.Shutdown(ctx); err != nil {
			log.Printf("Receiver shutdown error: %v", err)
		}
	}()

	// Initialize alerting subsystem.
	alertDBPath := *dataDir + "/alerting.json"
	if *dataDir == "" {
		alertDBPath = "alerting.json"
	}
	alertSub, err := alerting.InitAlerting(alertDBPath, store)
	if err != nil {
		log.Printf("Warning: alerting disabled: %v", err)
	}
	if alertSub != nil {
		defer alertSub.Shutdown()
	}

	// Initialize API router.
	traceHandler := api.NewTraceHandler(store)
	var metricsHandler *api.MetricsHandler
	if metricStore != nil {
		metricsHandler = api.NewMetricsHandler(metricStore)
	}
	dashboardHandler := api.NewDashboardHandler("")
	sessionHandler := api.NewSessionHandler(store)
	logHandler := api.NewLogHandler(store)
	pricingHandler := api.NewPricingHandler(store)
	llmConfigHandler := api.NewLLMConfigHandler(store)
		costHandler := api.NewCostHandler(store)
	var alertHandler http.Handler
	if alertSub != nil {
		alertHandler = alertSub.Handler
	}
	router := api.NewRouter(traceHandler, metricsHandler, dashboardHandler, sessionHandler, logHandler, pricingHandler, llmConfigHandler, alertHandler, costHandler)

	httpSrv := &http.Server{
		Addr:         apiAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start API server.
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server error: %v", err)
		}
	}()

	// Seed default pricing from config on startup.
	for _, m := range cfg.Pricing.Models {
		if err := store.UpsertModelPricing(context.Background(), m); err != nil {
			log.Printf("Warning: failed to seed pricing for %s: %v", m.ModelName, err)
		}
	}

	fmt.Println("Press Ctrl+C to stop.")

	// Wait for shutdown signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Labubu stopped.")
}

func runRetentionCleanup(ctx context.Context, store storage.Store, ret storage.RetentionConfig) {
	ticker := time.NewTicker(ret.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			deleted, spans, err := store.Purge(ctx, ret.MaxAge, ret.MaxCount)
			if err != nil {
				log.Printf("Trace cleanup error: %v", err)
			} else if deleted > 0 {
				log.Printf("Trace cleanup: removed %d traces, %d spans", deleted, spans)
			}
		case <-ctx.Done():
			return
		}
	}
}
