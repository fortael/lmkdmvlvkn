package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"lmkdmvlvkn/internal/ui"
)

func main() {
	p := tea.NewProgram(ui.New(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
