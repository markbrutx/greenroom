package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/markbrutx/moored/internal/auth"
	"github.com/markbrutx/moored/internal/config"
	"github.com/markbrutx/moored/internal/live"
	"github.com/markbrutx/moored/internal/retro"
	"github.com/markbrutx/moored/internal/store"
	"github.com/markbrutx/moored/internal/web"
)

var version = "dev"

const usage = `Moored: self-hosted retro rooms. Create a room, share the link, export for your agent.

Usage:
  moored [serve]        start the server
  moored backup FILE    write a consistent copy of the SQLite database to FILE
  moored health         exit 0 when the local server answers /healthz
  moored version        print the version

Configuration comes from MOORED_* environment variables; see README.md.
`

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "backup":
		if len(os.Args) != 3 {
			err = errors.New("usage: moored backup FILE")
		} else {
			err = backup(os.Args[2])
		}
	case "health":
		err = health()
	case "version", "--version", "-v":
		fmt.Println("moored", fullVersion())
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "moored:", err)
		os.Exit(1)
	}
}

func fullVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return "dev-" + s.Value[:7]
			}
		}
	}
	return version
}

func backup(file string) error {
	url := os.Getenv("MOORED_DATABASE_URL")
	if url == "" {
		url = "sqlite:moored.db"
	}
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s already exists; pick a new file name", file)
	}
	ctx := context.Background()
	db, err := store.Open(ctx, url)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Backup(ctx, file); err != nil {
		return err
	}
	fmt.Println("backup written to", file)
	return nil
}

func health() error {
	addr := os.Getenv("MOORED_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	scheme := "http"
	client := &http.Client{Timeout: 3 * time.Second}
	if os.Getenv("MOORED_TLS_CERT") != "" {
		scheme = "https"
		client.Transport = insecureLocalTransport()
	}
	resp, err := client.Get(scheme + "://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz answered %s", resp.Status)
	}
	return nil
}

func newLogger(cfg *config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func serve() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	a, err := auth.New(ctx, db)
	if err != nil {
		return err
	}
	hub := live.NewHub()
	app, err := web.New(cfg, db, hub, log)
	if err != nil {
		return err
	}
	pool, err := retro.LoadPool(cfg.Templates)
	if err != nil {
		return err
	}
	rooms, err := retro.New(app, a, pool, retro.DefaultLimits())
	if err != nil {
		return err
	}
	go rooms.Janitor(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	srv.RegisterOnShutdown(hub.Close)

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() {
		if cfg.TLSCert != "" {
			errc <- srv.ServeTLS(ln, cfg.TLSCert, cfg.TLSKey)
		} else {
			errc <- srv.Serve(ln)
		}
	}()
	log.Info("moored started", "version", fullVersion(), "addr", ln.Addr().String(),
		"tls", cfg.TLSCert != "", "room_ttl", cfg.RoomTTL.String())

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		log.Warn("shutdown", "err", err)
	}
	return nil
}
