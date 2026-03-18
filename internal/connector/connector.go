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

// ErrPermissionDenied is returned when a connector gets a 403 from the remote
// API, indicating the token lacks access to the named resource.
type ErrPermissionDenied struct {
	Resource string
}

func (e *ErrPermissionDenied) Error() string {
	return "permission denied: " + e.Resource
}

// DiscoveredItem represents a single item discovered from an external source.
// ParentExternalID + ParentSourceType identify the parent item if this item
// is a child of another discovered item (e.g. a label under a repo, or a
// child Notion page under its parent). Both fields must be set together.
// Items must be emitted parents-before-children for the discovery loop to
// correctly resolve parent catalogue IDs.
type DiscoveredItem struct {
	SourceType       string
	ExternalID       string
	Title            string
	URL              string
	SourceMeta       map[string]any
	Excerpt          string
	ParentExternalID string
	ParentSourceType string
}

// Discoverer is implemented by each source connector that supports discovery.
type Discoverer interface {
	Discover(ctx context.Context, target string) ([]DiscoveredItem, error)
}
