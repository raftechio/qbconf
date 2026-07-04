// Package logging constructs the application-wide zerolog logger.
package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/term"
)

// Log output formats accepted by New.
const (
	FormatAuto    = "auto"
	FormatJSON    = "json"
	FormatConsole = "console"
)

// New returns a logger writing to w at the given level. Format "console"
// forces human-readable output, "json" forces structured output, and "auto"
// picks console when w is a terminal and JSON otherwise (e.g. in CI).
func New(level, format string, w io.Writer) (zerolog.Logger, error) {
	lvl, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil {
		return zerolog.Nop(), fmt.Errorf("invalid log level %q: %w", level, err)
	}

	out := w
	switch format {
	case FormatJSON:
	case FormatConsole:
		out = zerolog.ConsoleWriter{Out: w, TimeFormat: time.RFC3339}
	case FormatAuto:
		if isTerminal(w) {
			out = zerolog.ConsoleWriter{Out: w, TimeFormat: time.RFC3339}
		}
	default:
		return zerolog.Nop(), fmt.Errorf("invalid log format %q (valid: %s, %s, %s)", format, FormatAuto, FormatJSON, FormatConsole)
	}

	return zerolog.New(out).Level(lvl).With().Timestamp().Logger(), nil
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
