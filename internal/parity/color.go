package parity

import (
	"io"
	"os"
)

const (
	red    = "31"
	green  = "32"
	yellow = "33"
)

// paint colors text for terminals. NO_COLOR turns colors off (no-color.org).
func paint(writer io.Writer, color, text string) string {
	if os.Getenv("NO_COLOR") != "" || !isTerminal(writer) {
		return text
	}
	return "\x1b[" + color + "m" + text + "\x1b[0m"
}
