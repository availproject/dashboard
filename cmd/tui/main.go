package main

import (
	"flag"
	"fmt"
)

func main() {
	serverAddr := flag.String("server", "http://localhost:8080", "dashboard server address")
	flag.Parse()

	fmt.Printf("dashboard-tui connecting to %s\n", *serverAddr)
}
