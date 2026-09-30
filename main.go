package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/titagaki/peercast-0yp/internal/archive"
	"github.com/titagaki/peercast-0yp/internal/channel"
	"github.com/titagaki/peercast-0yp/internal/config"
	"github.com/titagaki/peercast-0yp/internal/httpd"
	"github.com/titagaki/peercast-0yp/internal/pcp"
	"github.com/titagaki/peercast-0yp/internal/repository"
)

// loadDotEnv reads key=value pairs from .env and sets them as environment
// variables if they are not already set. Silently ignores missing file.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func main() {
	configPath := flag.String("config", "./peercast-0yp.toml", "path to config file")
	flag.Parse()

	loadDotEnv(".env")

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	db, err := sql.Open("pgx", cfg.Database.DSN)
	if err != nil {
		slog.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	store := channel.NewStoreWithGenrePrefix(cfg.PCP.GenrePrefix)

	srv, err := pcp.New(store, pcp.Config{
		MaxConnections:   cfg.PCP.MaxConnections,
		UpdateInterval:   time.Duration(cfg.PCP.UpdateInterval) * time.Second,
		HitTimeout:       time.Duration(cfg.PCP.HitTimeout) * time.Second,
		MinClientVersion: cfg.PCP.MinClientVersion,
	})
	if err != nil {
		slog.Error("failed to create PCP server", "err", err)
		os.Exit(1)
	}

	sessions := repository.NewSessionRepo(db, cfg.PCP.GenrePrefix)
	snapshots := repository.NewSnapshotRepo(db, cfg.PCP.GenrePrefix)

	rec := archive.New(sessions, snapshots, store, slog.Default())

	infoLines := make([]httpd.InfoLine, len(cfg.HTTP.Info))
	for i, il := range cfg.HTTP.Info {
		infoLines[i] = httpd.InfoLine{Name: il.Name, Comment: il.Comment, URL: il.URL}
	}
	httpdSrv := httpd.New(httpd.Config{
		Port:        cfg.HTTP.Port,
		CORSOrigins: cfg.HTTP.CORSOrigins,
		YPName:      cfg.HTTP.YPName,
		YPURL:       cfg.HTTP.YPURL,
		YPIndexURL:  cfg.HTTP.YPIndexURL,
		PCPAddress:  cfg.HTTP.PCPAddress,
		InfoLines:   infoLines,
	}, store, sessions, snapshots)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		rec.Start(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpdSrv.ListenAndServe(); err != nil {
			slog.Info("HTTP server stopped", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpdSrv.Shutdown(shutCtx)
	}()

	pcpAddr := fmt.Sprintf(":%d", cfg.PCP.Port)
	slog.Info("starting", "pcp", pcpAddr, "http", fmt.Sprintf(":%d", cfg.HTTP.Port))
	if err := srv.ListenAndServe(ctx, pcpAddr); err != nil {
		slog.Error("PCP server stopped with error", "err", err)
		os.Exit(1)
	}

	wg.Wait()
	slog.Info("server stopped")
}
