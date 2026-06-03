package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labubu/labubu/internal/api"
	"github.com/labubu/labubu/internal/pipeline"
	"github.com/labubu/labubu/internal/receiver"
	"github.com/labubu/labubu/internal/storage"
)

func main() {
	var (
		apiAddr       = flag.String("api-addr", "0.0.0.0:8080", "API and UI listen address")
		dataDir       = flag.String("data-dir", "./data", "chDB data directory (empty for in-memory)")
		bufferSize    = flag.Int("buffer-size", 1000, "pipeline buffer capacity")
		flushInterval = flag.Duration("flush-interval", 200*time.Millisecond, "pipeline flush interval")
	)
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Labubu starting...")

	// Initialize chDB storage.
	store, err := storage.NewChDBStore(*dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize chDB: %v", err)
	}
	defer store.Close()
	log.Printf("chDB initialized (data dir: %q)", *dataDir)

	// Initialize pipeline.
	pipe := pipeline.New(store, *bufferSize, *flushInterval)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pipe.Shutdown(ctx); err != nil {
			log.Printf("Pipeline shutdown error: %v", err)
		}
	}()
	log.Printf("Pipeline started (buffer: %d, flush: %v)", *bufferSize, *flushInterval)

	// Initialize OTLP receiver.
	recv := receiver.New(pipe, nil) // metricStore = nil until Task 8
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

	// Initialize API router.
	traceHandler := api.NewTraceHandler(store)
	router := api.NewRouter(traceHandler, nil) // metricsHandler = nil until Task 8

	httpSrv := &http.Server{
		Addr:         *apiAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start API server.
	go func() {
		log.Printf("API server listening on %s", *apiAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server error: %v", err)
		}
	}()

	// Wait for shutdown signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("Received signal %v, shutting down...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Labubu stopped.")
}
