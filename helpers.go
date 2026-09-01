package main

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

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

// maxInt returns the larger of two ints.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// minInt returns the smaller of two ints.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// repoWebURL converts a git remote to a browser URL
// (git@github.com:user/repo.git → https://github.com/user/repo).
func repoWebURL(remote string) string {
	r := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	if r == "" {
		return ""
	}
	if strings.HasPrefix(r, "ssh://git@") {
		return "https://" + strings.TrimPrefix(r, "ssh://git@")
	}
	if strings.HasPrefix(r, "git@") {
		if i := strings.Index(r, ":"); i > 0 {
			return "https://" + strings.TrimPrefix(r[:i], "git@") + "/" + r[i+1:]
		}
	}
	if strings.HasPrefix(r, "http://") || strings.HasPrefix(r, "https://") {
		return r
	}
	return ""
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

// wrapText word-wraps s to width w, preserving existing newlines.
func wrapText(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range words {
			for len([]rune(word)) > w { // hard-break oversized words
				if line != "" {
					out = append(out, line)
					line = ""
				}
				r := []rune(word)
				out = append(out, string(r[:w]))
				word = string(r[w:])
			}
			if line == "" {
				line = word
			} else if len([]rune(line))+1+len([]rune(word)) <= w {
				line += " " + word
			} else {
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// ansiTail returns s from visible column col onward, re-asserting whatever
// SGR styling was active at the cut point so colours survive the split.
func ansiTail(s string, col int) string {
	if col <= 0 {
		return s
	}
	var out strings.Builder
	var sgr []string
	emitting := false
	pos := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			seq := s[i:j]
			if strings.HasSuffix(seq, "m") {
				if seq == "\x1b[0m" || seq == "\x1b[m" {
					sgr = sgr[:0]
				} else {
					sgr = append(sgr, seq)
				}
			}
			if emitting {
				out.WriteString(seq)
			}
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		rw := runewidth.RuneWidth(r)
		if emitting {
			out.WriteRune(r)
		} else if pos+rw > col {
			emitting = true
			out.WriteString(strings.Join(sgr, ""))
			if pos+rw > col && pos < col {
				// A wide rune straddles the cut — replace its visible half.
				out.WriteString(strings.Repeat(" ", pos+rw-col))
			} else {
				out.WriteRune(r)
			}
		}
		pos += rw
		i += size
	}
	return out.String()
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
