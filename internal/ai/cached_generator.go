package ai

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/store"
)

// CachedGenerator wraps a Generator and caches results in the store keyed by
// SHA-256 of the canonical (sorted-key) JSON of inputs + annotations.
type CachedGenerator struct {
	inner Generator
	store *store.Store
}

// NewCachedGenerator returns a CachedGenerator backed by the given inner Generator and Store.
func NewCachedGenerator(inner Generator, s *store.Store) *CachedGenerator {
	return &CachedGenerator{inner: inner, store: s}
}

// Generate checks the cache for a prior result; on a miss it calls the inner
// Generator and stores the result before returning.
func (c *CachedGenerator) Generate(ctx context.Context, pipeline string, teamID *int64, inputs map[string]any, annotations []store.Annotation) (string, error) {
	canonical, err := canonicalJSON(inputs, annotations)
	if err != nil {
		return "", fmt.Errorf("cached_generator: canonicalize: %w", err)
	}

	h := sha256.Sum256([]byte(canonical))
	inputHash := hex.EncodeToString(h[:])

	var nullTeamID sql.NullInt64
	if teamID != nil {
		nullTeamID = sql.NullInt64{Int64: *teamID, Valid: true}
	}

	entry, err := c.store.GetCacheEntry(ctx, inputHash, pipeline, nullTeamID)
	if err == nil {
		return entry.Output, nil
	}

	// If the caller placed the full prompt in inputs["prompt"], send that to the
	// inner generator directly. Otherwise fall back to the canonical JSON so the
	// inner generator still receives a deterministic, content-rich string.
	prompt := canonical
	if p, ok := inputs["prompt"]; ok {
		if ps, ok := p.(string); ok && ps != "" {
			prompt = ps
		}
	}

	output, err := c.inner.Generate(ctx, prompt)
	if err != nil {
		return "", err
	}

	if _, err := c.store.SetCacheEntry(ctx, inputHash, pipeline, nullTeamID, output); err != nil {
		return "", fmt.Errorf("cached_generator: store: %w", err)
	}
	return output, nil
}

// canonicalJSON serializes inputs and annotations to deterministic JSON.
// encoding/json sorts map keys alphabetically at every level.
func canonicalJSON(inputs map[string]any, annotations []store.Annotation) (string, error) {
	obj := map[string]any{
		"annotations": annotations,
		"inputs":      inputs,
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
