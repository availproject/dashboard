package ai

import "context"

// Generator is the interface for AI text generation.
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}
