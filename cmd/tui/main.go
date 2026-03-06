package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui"
	"github.com/your-org/dashboard/internal/tui/client"
	"github.com/your-org/dashboard/internal/tui/views"
)

func main() {
	serverAddr := flag.String("server", "http://localhost:8080", "dashboard server address")
	flag.Parse()

	c := client.New(*serverAddr)

	app := tui.NewApp(c)

	// Attempt to load a saved token. Push the login view if none exists or it
	// has expired; views pushed on top of login will be added by later stories.
	needLogin := true
	if err := c.LoadToken(); err == nil && !c.IsTokenExpired() {
		needLogin = false
	}

	if needLogin {
		app.PushView(views.NewLoginView(c))
	}

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		log.Fatal(err)
	}
}
