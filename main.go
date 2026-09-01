package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if handled, code := RunCLI(os.Args[1:]); handled {
		os.Exit(code)
	}

	app := NewApp()
	// Cell motion (not all-motion): click/release/drag only. All-motion floods
	// the input parser on fast mouse moves and fragments leak into text inputs.
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
