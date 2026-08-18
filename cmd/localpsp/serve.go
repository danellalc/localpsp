package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danellalc/localpsp/dispatch"
	"github.com/danellalc/localpsp/engine"
	"github.com/danellalc/localpsp/providers/asaas"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8420", "address to listen on")
	seed := fs.Int64("seed", 1, "seed driving every generated id; the same seed and call sequence always produce the same ids")
	dsn := fs.String("persist", "", "SQLite file path to persist state across restarts; empty uses an in-memory database")
	basePath := fs.String("asaas-base-path", "/asaas/v3", "base path the Asaas facade mounts under")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	eng, err := engine.New(ctx, engine.Options{Seed: *seed, StartTime: time.Now(), DSN: *dsn})
	if err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	defer func() { _ = eng.Close() }()

	disp := dispatch.New(dispatch.Options{})
	defer func() { _ = disp.Close() }()

	srv := asaas.NewServer(eng, disp, *basePath, publicURLFor(*addr))
	httpServer := &http.Server{Addr: *addr, Handler: srv}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("localpsp: listening on %s (asaas facade at %s)\n", *addr, *basePath)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-sigCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

// publicURLFor turns a listen address like ":8420" or "0.0.0.0:8420" into
// a URL that actually resolves from the same machine, "http://localhost:8420".
// A specific, non-wildcard host is kept as-is.
func publicURLFor(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}
