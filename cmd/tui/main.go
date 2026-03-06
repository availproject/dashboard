package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui"
	"github.com/your-org/dashboard/internal/tui/client"
)

func main() {
	serverAddr := flag.String("server", "http://localhost:8080", "dashboard server address")
	flag.Parse()

	c := client.New(*serverAddr)

	app := tui.NewApp(c)

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		log.Fatal(err)
	}
}
