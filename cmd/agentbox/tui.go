package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

var (
	// Theme colors
	colorError   = lipgloss.Color("#E84855") // Coral Red
	colorWarning = lipgloss.Color("#FFB03B") // Amber
	colorInfo    = lipgloss.Color("#00A8CC") // Blue-Green Cyan
	colorSuccess = lipgloss.Color("#4CAF50") // Green
	colorText    = lipgloss.Color("#ECEFF4") // Light text

	// Badges
	badgeWarning = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(colorWarning).
			Padding(0, 1).
			Render(" WARNING ")

	badgeInfo = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(colorInfo).
			Padding(0, 1).
			Render(" INFO ")

	badgeSuccess = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(colorSuccess).
			Padding(0, 1).
			Render(" SUCCESS ")
)

// LogInfo prints a stylized informational message.
func LogInfo(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(os.Stderr, "%s %s\n", badgeInfo, msg)
}

// LogSuccess prints a stylized success message.
func LogSuccess(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(os.Stderr, "%s %s\n", badgeSuccess, msg)
}

// LogWarning prints a stylized warning message.
func LogWarning(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(os.Stderr, "%s %s\n", badgeWarning, msg)
}

// RenderError prints a beautifully formatted error box.
func RenderError(err error) {
	if err == nil {
		return
	}

	isTTY := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())

	if !isTTY {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return
	}

	errorBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorError).
		Padding(1, 2).
		Margin(1, 0, 1, 1).
		Width(72)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorError).
		Render("✖ Execution Failed")

	msg := lipgloss.NewStyle().
		Foreground(colorText).
		Render(err.Error())

	fmt.Fprintln(os.Stderr, errorBoxStyle.Render(title+"\n\n"+msg))
}

// Spinner represents a TUI loading spinner.
type Spinner struct {
	message string
	mu      sync.RWMutex
	done    chan struct{}
	wg      sync.WaitGroup
	isTTY   bool
	writer  io.Writer
}

// NewSpinner creates a new Spinner.
func NewSpinner(message string) *Spinner {
	return &Spinner{
		message: message,
		done:    make(chan struct{}),
		isTTY:   isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd()),
		writer:  os.Stderr,
	}
}

// Start starts the spinner animation.
func (s *Spinner) Start() {
	if !s.isTTY {
		s.mu.RLock()
		msg := s.message
		s.mu.RUnlock()
		fmt.Fprintf(s.writer, "⌛ %s...\n", msg)
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		
		// Hide the cursor
		fmt.Fprint(s.writer, "\033[?25l")
		defer fmt.Fprint(s.writer, "\033[?25h") // Restore cursor

		for {
			select {
			case <-s.done:
				// Clear the line
				fmt.Fprint(s.writer, "\r\033[K")
				return
			default:
				s.mu.RLock()
				msg := s.message
				s.mu.RUnlock()
				frame := lipgloss.NewStyle().Foreground(colorInfo).Render(frames[i])
				fmt.Fprintf(s.writer, "\r%s  %s", frame, msg)
				i = (i + 1) % len(frames)
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

// SetMessage updates the spinner message thread-safely.
func (s *Spinner) SetMessage(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
	if !s.isTTY {
		fmt.Fprintf(s.writer, "⌛ %s...\n", message)
	}
}

// Stop stops the spinner and prints a final status.
func (s *Spinner) Stop(success bool, finalMsg string) {
	if !s.isTTY {
		if success {
			fmt.Fprintf(s.writer, "✔ %s\n", finalMsg)
		} else {
			fmt.Fprintf(s.writer, "✖ %s\n", finalMsg)
		}
		return
	}

	close(s.done)
	s.wg.Wait()

	if success {
		check := lipgloss.NewStyle().Foreground(colorSuccess).Render("✔")
		fmt.Fprintf(s.writer, "\r%s  %s\n", check, finalMsg)
	} else {
		cross := lipgloss.NewStyle().Foreground(colorError).Render("✖")
		fmt.Fprintf(s.writer, "\r%s  %s\n", cross, finalMsg)
	}
}
