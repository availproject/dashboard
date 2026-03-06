package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/your-org/dashboard/internal/api"
	"github.com/your-org/dashboard/internal/auth"
	"github.com/your-org/dashboard/internal/config"
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

	deps := &api.Deps{
		Store:  st,
		Config: cfg,
	}

	router := api.NewRouter(deps)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
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
