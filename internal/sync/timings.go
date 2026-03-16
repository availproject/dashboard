package sync

import (
	"encoding/json"
	"sync"
	"time"
)

// syncTimings is a thread-safe collector of step timings for a single sync run.
// Keys are human-readable step names; values are milliseconds elapsed.
type syncTimings struct {
	mu      sync.Mutex
	entries map[string]int64
}

func newSyncTimings() *syncTimings {
	return &syncTimings{entries: make(map[string]int64)}
}

// record stores the elapsed time since t0 under the given key.
func (t *syncTimings) record(key string, t0 time.Time) {
	ms := time.Since(t0).Milliseconds()
	t.mu.Lock()
	t.entries[key] = ms
	t.mu.Unlock()
}

// set stores an explicit millisecond value under the given key.
func (t *syncTimings) set(key string, ms int64) {
	t.mu.Lock()
	t.entries[key] = ms
	t.mu.Unlock()
}

// toJSON serializes all entries to a JSON object string.
func (t *syncTimings) toJSON() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, _ := json.Marshal(t.entries)
	return string(b)
}
