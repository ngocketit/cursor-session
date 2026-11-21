package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	progressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)
)

// ProgressStep represents a single step in a multi-step process
type ProgressStep struct {
	Message string
	Fn      func() error
}

// ShowProgress runs a spinner with a message
func ShowProgress(ctx context.Context, message string, fn func() error) error {
	// Check if we're in a TTY
	if !isTerminal(os.Stderr) {
		// Not a TTY, just run the function
		LogInfo(message)
		return fn()
	}

	return showProgressSimple(ctx, message, fn)
}

// ShowProgressWithSteps shows progress for multiple steps
func ShowProgressWithSteps(ctx context.Context, steps []ProgressStep) error {
	if !isTerminal(os.Stderr) {
		// Not a TTY, just run steps sequentially
		for _, step := range steps {
			LogInfo(step.Message)
			if err := step.Fn(); err != nil {
				return fmt.Errorf("%s: %w", step.Message, err)
			}
		}
		return nil
	}

	return showProgressWithStepsSimple(ctx, steps)
}

// showProgressSimple uses a simple text-based spinner
func showProgressSimple(ctx context.Context, message string, fn func() error) error {
	spinnerChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	done := make(chan error, 1)
	stopSpinner := make(chan struct{})
	spinnerDone := make(chan struct{})

	// Start spinner
	go func() {
		defer close(spinnerDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopSpinner:
				return
			case <-ticker.C:
				char := spinnerChars[i%len(spinnerChars)]
				fmt.Fprintf(os.Stderr, "\r%s %s", progressStyle.Render(char), message)
				i++
			}
		}
	}()

	// Run the function
	go func() {
		done <- fn()
	}()

	// Wait for function or context
	select {
	case err := <-done:
		close(stopSpinner)
		<-spinnerDone
		if err != nil {
			fmt.Fprintf(os.Stderr, "\r%s %s\n", errorStyle.Render("✗"), message)
			return err
		}
		fmt.Fprintf(os.Stderr, "\r%s %s\n", successStyle.Render("✓"), message)
		return nil
	case <-ctx.Done():
		close(stopSpinner)
		<-spinnerDone
		return ctx.Err()
	}
}

// showProgressWithStepsSimple uses simple output for multi-step progress
func showProgressWithStepsSimple(ctx context.Context, steps []ProgressStep) error {
	for i, step := range steps {
		msg := fmt.Sprintf("[%d/%d] %s", i+1, len(steps), step.Message)
		if err := showProgressSimple(ctx, msg, step.Fn); err != nil {
			return err
		}
	}
	return nil
}

// gumAvailable checks if gum is available
func gumAvailable() bool {
	_, err := exec.LookPath("gum")
	return err == nil
}

// isTerminal checks if the writer is a terminal
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		stat, err := f.Stat()
		if err != nil {
			return false
		}
		return (stat.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// PrintSuccess prints a success message
func PrintSuccess(message string) {
	if isTerminal(os.Stdout) {
		fmt.Printf("%s %s\n", successStyle.Render("✓"), message)
	} else {
		fmt.Println(message)
	}
}

// PrintError prints an error message
func PrintError(message string) {
	if isTerminal(os.Stderr) {
		fmt.Fprintf(os.Stderr, "%s %s\n", errorStyle.Render("✗"), message)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", message)
	}
}

// PrintInfo prints an info message
func PrintInfo(message string) {
	if isTerminal(os.Stdout) {
		fmt.Printf("%s %s\n", progressStyle.Render("ℹ"), message)
	} else {
		fmt.Println(message)
	}
}

// PrintWarning prints a warning message
func PrintWarning(message string) {
	if isTerminal(os.Stderr) {
		fmt.Fprintf(os.Stderr, "%s %s\n", warningStyle.Render("⚠"), message)
	} else {
		fmt.Fprintf(os.Stderr, "WARNING: %s\n", message)
	}
}
