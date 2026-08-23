package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.com/backflash-cli/backflash/internal/config"
	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/store"
	"github.com/backflash-cli/backflash/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "0.1.0"
var commit = "dev"
var built = "okänt"

func main() {
	paths := config.DefaultPaths()
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("BACKFLASH %s\ncommit: %s\nbyggd: %s\ngo: %s\nos/arkitektur: %s/%s\n", version, commit, built, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return
	}
	tui.Splash(os.Stdout, terminalWidth())
	s, err := store.Open(paths.Database)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Kunde inte öppna databasen:", err)
		os.Exit(1)
	}
	defer s.Close()
	c := flashback.NewClient(flashback.AnonymousSession{})
	program := tea.NewProgram(tui.New(s, c), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "BACKFLASH avslutades med fel:", err)
		os.Exit(1)
	}
}

func terminalWidth() int {
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		return n
	}
	return 120
}
