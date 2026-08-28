package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/backflash-cli/backflash/internal/config"
	"github.com/backflash-cli/backflash/internal/diagnostics"
	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/store"
	"github.com/backflash-cli/backflash/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "0.1.0"
var commit = "dev"
var built = "okänt"

func main() {
	finish := diagnostics.Start("launcher")
	defer finish()
	paths := config.DefaultPaths()
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("BACKFLASH %s\ncommit: %s\nbyggd: %s\ngo: %s\nos/arkitektur: %s/%s\n", version, commit, built, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return
	}
	splashDone := diagnostics.Start("splash")
	tui.Splash(os.Stdout, terminalWidth())
	splashDone()
	dbDone := diagnostics.Start("database open/migrate")
	s, err := store.Open(paths.Database)
	dbDone()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Kunde inte öppna databasen:", err)
		os.Exit(1)
	}
	defer s.Close()
	if os.Getenv("BACKFLASH_GANDR_DEBUG") != "" {
		logPath := filepath.Join(filepath.Dir(paths.Database), "gandr-debug.log")
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600); err == nil {
			log.SetOutput(f)
			defer f.Close()
		}
	}
	c := flashback.NewClient(flashback.AnonymousSession{})
	appDone := diagnostics.Start("app construction")
	model := tui.New(s, c)
	appDone()
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	finalModel, runErr := program.Run()
	if app, ok := finalModel.(tui.App); ok {
		if shutdownErr := app.Shutdown(); shutdownErr != nil {
			fmt.Fprintln(os.Stderr, "Mesh kunde inte stängas rent:", shutdownErr)
		}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "BACKFLASH avslutades med fel:", runErr)
		os.Exit(1)
	}
}

func terminalWidth() int {
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		return n
	}
	return 120
}
