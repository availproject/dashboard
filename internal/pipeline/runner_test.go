package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/config"
	"github.com/your-org/dashboard/internal/store"
)

// TestRunSprintParseIntegration calls the real ClaudeCodeProvider with a short
// sprint plan sample and asserts that at least one goal is returned.
// The test is skipped when the `claude` binary is not found in PATH.
func TestRunSprintParseIntegration(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude binary not found in PATH; skipping integration test")
	}
	if os.Getenv("CLAUDECODE") != "" {
		t.Skip("inside a Claude Code session; skipping integration test to avoid nested session")
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	provider, err := ai.New(config.AIConfig{Provider: "claude-code"})
	if err != nil {
		t.Fatalf("ai.New: %v", err)
	}
	cached := ai.NewCachedGenerator(provider, s)
	runner := New(cached, s)

	// Create a minimal team so the teamID is valid in the store.
	team, err := s.CreateTeam(context.Background(), "Test Team")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}

	const samplePlan = `Sprint 1 of 4
Start date: 2026-03-03

Goals:
- Ship user authentication
- Add dashboard overview page
- Fix critical performance bug in data loader`

	result, err := runner.RunSprintParse(context.Background(), team.ID, samplePlan)
	if err != nil {
		t.Fatalf("RunSprintParse: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Goals) == 0 {
		t.Error("expected at least one goal in result")
	}
}
