package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// ClaudeCodeProvider calls the local `claude` CLI binary.
type ClaudeCodeProvider struct {
	binaryPath string
	model      string
}

func newClaudeCodeProvider(binaryPath, model string) *ClaudeCodeProvider {
	if binaryPath == "" {
		binaryPath = "claude"
	}
	return &ClaudeCodeProvider{binaryPath: binaryPath, model: model}
}

func (c *ClaudeCodeProvider) Generate(ctx context.Context, prompt string) (string, error) {
	args := []string{
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
	}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, c.binaryPath, args...) //nolint:gosec
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("claude-code: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("claude-code: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("claude-code: start %q: %w", c.binaryPath, err)
	}

	var wg sync.WaitGroup
	var output strings.Builder
	var stderrBuf strings.Builder

	wg.Add(2)
	go func() {
		defer wg.Done()
		parseStreamJSON(stdout, &output)
	}()
	go func() {
		defer wg.Done()
		drainReader(stderr, &stderrBuf)
	}()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if msg := stderrBuf.String(); msg != "" {
			return "", fmt.Errorf("claude-code: %w\nstderr: %s", err, msg)
		}
		return "", fmt.Errorf("claude-code: %w", err)
	}
	return output.String(), nil
}

func parseStreamJSON(r io.Reader, out *strings.Builder) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var event struct {
			Type   string `json:"type"`
			Result string `json:"result"`
			Delta  struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		switch event.Type {
		case "result":
			if event.Result != "" {
				out.WriteString(event.Result)
			}
		case "content_block_delta":
			if t := event.Delta.Text; t != "" {
				out.WriteString(t)
			}
		}
	}
}

func drainReader(r io.Reader, buf *strings.Builder) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			buf.WriteString(line + "\n")
		}
	}
}
