package components

import "github.com/charmbracelet/lipgloss"

var (
	bannerInProgressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	bannerFailedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
)

// Banner holds the current sync banner state.
type Banner struct {
	Active    bool
	Label     string
	Failed    bool
	FailedMsg string
}

// Render returns the banner string (empty when not active).
func (b *Banner) Render() string {
	if !b.Active {
		return ""
	}
	if b.Failed {
		return bannerFailedStyle.Render("  [ Sync failed: "+b.FailedMsg+" ]  Press Enter to dismiss") + "\n"
	}
	label := b.Label
	if label != "" {
		label = " (" + label + ")"
	}
	return bannerInProgressStyle.Render("  [ Sync in progress..."+label+" ]") + "\n"
}
