package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// AnthropicProvider calls the Anthropic REST API.
type AnthropicProvider struct {
	apiKey string
	model  string
}

func newAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if envKey := os.Getenv("ANTHROPIC_API_KEY"); envKey != "" {
		apiKey = envKey
	}
	return &AnthropicProvider{apiKey: apiKey, model: model}
}

func (a *AnthropicProvider) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":      a.model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: http: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anthropic: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, data)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("anthropic: parse response: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("anthropic: empty content in response")
	}
	return result.Content[0].Text, nil
}
