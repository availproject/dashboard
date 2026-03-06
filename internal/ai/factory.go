package ai

import (
	"fmt"

	"github.com/your-org/dashboard/internal/config"
)

// New returns the Generator configured by cfg.
func New(cfg config.AIConfig) (Generator, error) {
	switch cfg.Provider {
	case "claude-code":
		return newClaudeCodeProvider(cfg.BinaryPath, cfg.Model), nil
	case "anthropic":
		return newAnthropicProvider(cfg.APIKey, cfg.Model), nil
	default:
		return nil, fmt.Errorf("ai: unknown provider %q", cfg.Provider)
	}
}
