package msgs

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// PushViewMsg is sent by any view to push a new view onto the App stack.
type PushViewMsg struct{ View tea.Model }

// PopViewMsg is sent to pop the current view from the App stack.
type PopViewMsg struct{}

// SourcePickedMsg is sent by ConfigSourcePickerView when the user selects an item.
// The picker pops itself and then sends this message so the parent view can handle it.
type SourcePickedMsg struct {
	Item client.SourceItemResponse
}

// DiscoveredItemsSelectedMsg is sent by ConfigDiscoverInlineView when the user
// confirms discovered items to add.
type DiscoveredItemsSelectedMsg struct {
	Items []client.SourceItemResponse
}
