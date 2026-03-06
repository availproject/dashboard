package msgs

import tea "github.com/charmbracelet/bubbletea"

// PushViewMsg is sent by any view to push a new view onto the App stack.
type PushViewMsg struct{ View tea.Model }

// PopViewMsg is sent to pop the current view from the App stack.
type PopViewMsg struct{}
