package connector

import "context"

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
