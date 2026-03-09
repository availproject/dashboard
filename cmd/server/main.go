package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/api"
	"github.com/your-org/dashboard/internal/auth"
	"github.com/your-org/dashboard/internal/config"
	ghconn "github.com/your-org/dashboard/internal/connector/github"
	"github.com/your-org/dashboard/internal/connector/grafana"
	"github.com/your-org/dashboard/internal/connector/notion"
	"github.com/your-org/dashboard/internal/connector/posthog"
	"github.com/your-org/dashboard/internal/connector/signoz"
	"github.com/your-org/dashboard/internal/pipeline"
	enginesync "github.com/your-org/dashboard/internal/sync"
	"github.com/your-org/dashboard/internal/store"
)

func main() {
	// hash-password subcommand: reads password from stdin, prints bcrypt hash, exits.
	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		fmt.Print("Password: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		password := strings.TrimSpace(scanner.Text())
		hash, err := auth.HashPassword(password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(hash)
		return
	}

	configPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if err := config.LoadEnv(".env"); err != nil {
		log.Fatalf("failed to load .env: %v", err)
	}

	fmt.Println("dashboard-server starting")
	fmt.Printf("  server.port:             %d\n", cfg.Server.Port)
	fmt.Printf("  storage.path:            %s\n", cfg.Storage.Path)
	fmt.Printf("  auth.jwt_secret:         %s\n", mask(cfg.Auth.JWTSecret))
	fmt.Printf("  auth.admin_username:     %s\n", cfg.Auth.AdminUsername)
	fmt.Printf("  auth.admin_password_hash:%s\n", mask(cfg.Auth.AdminPasswordHash))
	fmt.Printf("  ai.provider:             %s\n", cfg.AI.Provider)
	fmt.Printf("  ai.model:                %s\n", cfg.AI.Model)
	fmt.Printf("  ai.api_key:              %s\n", mask(cfg.AI.APIKey))
	fmt.Printf("  ai.binary_path:          %s\n", cfg.AI.BinaryPath)
	fmt.Printf("  GITHUB_TOKEN:            %s\n", mask(config.GitHubToken()))
	fmt.Printf("  NOTION_TOKEN:            %s\n", mask(config.NotionToken()))

	st, err := store.New(cfg.Storage.Path)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	if err := auth.Bootstrap(st, cfg); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	// Prune stale AI cache entries older than 30 days.
	if err := st.PruneStaleCache(context.Background(), 30*24*time.Hour); err != nil {
		log.Printf("warn: prune stale cache: %v", err)
	}

	// Build AI generator.
	gen, err := ai.New(cfg.AI)
	if err != nil {
		log.Printf("warn: AI generator unavailable: %v", err)
	}

	// Build connectors.
	gh := ghconn.New(config.GitHubToken())
	no := notion.New(config.NotionToken())
	gr := grafana.New(config.GrafanaToken(), config.GrafanaBaseURL())
	ph := posthog.New(config.PostHogAPIKey(), config.PostHogHost())
	si := signoz.New(config.SignozAPIKey(), config.SignozBaseURL())

	// Build pipeline runner.
	var cachedGen *ai.CachedGenerator
	if gen != nil {
		cachedGen = ai.NewCachedGenerator(gen, st)
	}
	pl := pipeline.New(cachedGen, st)

	// Build sync engine.
	engine := enginesync.New(st, gh, no, gr, ph, si, pl)

	deps := &api.Deps{
		Store:  st,
		Config: cfg,
		Engine: engine,
	}

	// Signal context for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// WaitGroup to track in-progress background goroutines (autotag ticker, HTTP server).
	var wg sync.WaitGroup

	// AutoTag ticker: runs every hour.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("autotag: running scheduled auto-tag")
				if err := engine.AutoTag(ctx); err != nil {
					log.Printf("autotag: %v", err)
				}
			}
		}
	}()

	// HTTP server with graceful shutdown.
	router := api.NewRouter(deps)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Start server in background.
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	// Wait for shutdown signal.
	<-ctx.Done()
	log.Println("shutting down…")

	// Give the HTTP server up to 30 seconds to finish in-flight requests.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}

	// Wait for background goroutines (autotag ticker).
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		log.Println("timeout waiting for goroutines; forcing exit")
	}

	log.Println("bye")
}

func mask(s string) string {
	if len(s) == 0 {
		return "(not set)"
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}
