package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/your-org/dashboard/internal/config"
)

func main() {
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
