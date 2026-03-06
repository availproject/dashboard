package ai

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/your-org/dashboard/internal/store"
)

// mockGenerator records calls and returns a preset response.
type mockGenerator struct {
	calls  int
	output string
	err    error
}

func (m *mockGenerator) Generate(_ context.Context, _ string) (string, error) {
	m.calls++
	return m.output, m.err
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("newTestStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCachedGenerator_CacheMiss(t *testing.T) {
	ctx := context.Background()
	mock := &mockGenerator{output: "ai-result"}
	s := newTestStore(t)
	cg := NewCachedGenerator(mock, s)

	inputs := map[string]any{"prompt": "hello"}
	out, err := cg.Generate(ctx, "concerns", nil, inputs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ai-result" {
		t.Errorf("expected 'ai-result', got %q", out)
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 inner call, got %d", mock.calls)
	}
}

func TestCachedGenerator_CacheHit(t *testing.T) {
	ctx := context.Background()
	mock := &mockGenerator{output: "ai-result"}
	s := newTestStore(t)
	cg := NewCachedGenerator(mock, s)

	inputs := map[string]any{"prompt": "hello"}
	// First call: cache miss, inner called.
	if _, err := cg.Generate(ctx, "concerns", nil, inputs, nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call: cache hit, inner NOT called.
	out, err := cg.Generate(ctx, "concerns", nil, inputs, nil)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if out != "ai-result" {
		t.Errorf("expected 'ai-result', got %q", out)
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 inner call total (cache hit), got %d", mock.calls)
	}
}

func TestCachedGenerator_DifferentInputsDifferentHashes(t *testing.T) {
	ctx := context.Background()
	mock := &mockGenerator{output: "result"}
	s := newTestStore(t)
	cg := NewCachedGenerator(mock, s)

	inputs1 := map[string]any{"prompt": "hello"}
	inputs2 := map[string]any{"prompt": "world"}

	if _, err := cg.Generate(ctx, "workload", nil, inputs1, nil); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if _, err := cg.Generate(ctx, "workload", nil, inputs2, nil); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	// Both calls are misses; inner should have been called twice.
	if mock.calls != 2 {
		t.Errorf("expected 2 inner calls (different inputs), got %d", mock.calls)
	}
}

func TestCachedGenerator_TeamIDAffectsCache(t *testing.T) {
	ctx := context.Background()
	mock := &mockGenerator{output: "result"}
	s := newTestStore(t)
	cg := NewCachedGenerator(mock, s)

	inputs := map[string]any{"prompt": "same"}
	team, err := s.CreateTeam(ctx, "test-team")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	teamID := team.ID

	if _, err := cg.Generate(ctx, "workload", nil, inputs, nil); err != nil {
		t.Fatalf("call without team: %v", err)
	}
	if _, err := cg.Generate(ctx, "workload", &teamID, inputs, nil); err != nil {
		t.Fatalf("call with team: %v", err)
	}
	// Same inputs but different teamID → different cache keys → 2 inner calls.
	if mock.calls != 2 {
		t.Errorf("expected 2 inner calls (different teamID), got %d", mock.calls)
	}
}
