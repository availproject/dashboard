package connector

import (
	"context"
	"fmt"
)

// ErrCredentialsMissing is returned by a connector when a required environment
// variable or credential is not set.
type ErrCredentialsMissing struct {
	VarName string
}

func (e *ErrCredentialsMissing) Error() string {
	return fmt.Sprintf("missing required credential: %s", e.VarName)
}

// NewErrCredentialsMissing creates an ErrCredentialsMissing for the given env var.
func NewErrCredentialsMissing(varName string) error {
	return &ErrCredentialsMissing{VarName: varName}
}

// DiscoveredItem represents a single item discovered from an external source.
type DiscoveredItem struct {
	SourceType string
	ExternalID string
	Title      string
	URL        string
	SourceMeta map[string]any
	Excerpt    string
}

// Discoverer is implemented by each source connector that supports discovery.
type Discoverer interface {
	Discover(ctx context.Context, target string) ([]DiscoveredItem, error)
}
