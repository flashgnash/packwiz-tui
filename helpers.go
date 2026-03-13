package main

import "github.com/charmbracelet/lipgloss"

// styleMuted renders s in the muted colour.
func styleMuted(s string) string {
	return lipgloss.NewStyle().Foreground(colorMuted).Render(s)
}

// stripAnsi is a passthrough; lipgloss.Width handles ANSI internally.
func stripAnsi(s string) string {
	return s
}

// truncate shortens s to max runes, adding an ellipsis if needed.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// clamp returns n clamped to [lo, hi].
func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// visibleWindow returns [start, end) for a scrollable list.
func visibleWindow(selected, total, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	start := selected - height/2
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > total {
		end = total
		start = end - height
		if start < 0 {
			start = 0
		}
	}
	return start, end
}
