package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mock-server/internal/manifest"
	"mock-server/internal/server"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	routesPath, err := manifest.ResolveRoutesPath()
	if err != nil {
		logger.Fatalf("resolve routes path: %v", err)
	}

	mockServer, err := server.New(routesPath, logger)
	if err != nil {
		logger.Fatalf("load routes manifest: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if server.IsTruthyEnv(os.Getenv(server.WatchEnv)) {
		go mockServer.Watch(ctx, time.Second)
	}

	httpServer := &http.Server{
		Addr:              ":8000",
		Handler:           mockServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Printf("mock server listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("server failed: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Fatalf("shutdown failed: %v", err)
	}
}
