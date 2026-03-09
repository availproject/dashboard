package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port int `yaml:"port"`
}

type StorageConfig struct {
	Path string `yaml:"path"`
}

type AuthConfig struct {
	JWTSecret         string `yaml:"jwt_secret"`
	AdminUsername     string `yaml:"admin_username"`
	AdminPasswordHash string `yaml:"admin_password_hash"`
}

type AIConfig struct {
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	APIKey     string `yaml:"api_key"`
	BinaryPath string `yaml:"binary_path"`
}

type MCPConfig struct {
	APIKey string `yaml:"api_key"`
}

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
	Auth    AuthConfig    `yaml:"auth"`
	AI      AIConfig      `yaml:"ai"`
	MCP     MCPConfig     `yaml:"mcp"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
