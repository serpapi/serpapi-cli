// Package spinner provides a TTY-aware activity spinner for long-running operations.
package spinner

import (
	"os"
	"time"

	"github.com/briandowns/spinner"
	"golang.org/x/term"
)

// Spinner wraps briandowns/spinner with TTY awareness.
type Spinner struct {
	s *spinner.Spinner
}

// New creates a spinner that writes to stderr.
// Returns a no-op spinner when stderr is not a TTY (pipes, CI, redirects).
func New(label string) *Spinner {
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return &Spinner{}
	}
	// CharSets[14] is the braille dots pattern: ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏
	s := spinner.New(spinner.CharSets[14], 80*time.Millisecond,
		spinner.WithWriter(os.Stderr),
		spinner.WithColor("cyan"),
	)
	s.Suffix = " " + label
	return &Spinner{s: s}
}

// Start begins the spinner animation.
func (sp *Spinner) Start() {
	if sp.s != nil {
		sp.s.Start()
	}
}

// Stop halts the spinner and clears it from the terminal.
func (sp *Spinner) Stop() {
	if sp.s != nil {
		sp.s.Stop()
	}
}
