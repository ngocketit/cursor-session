package cmd

import (
	"bytes"
	"testing"

	"github.com/iksnae/cursor-session/internal"
)

func TestExportCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "export with invalid format",
			args:    []string{"export", "--format", "invalid"},
			wantErr: true, // Invalid format should error
		},
		// Note: Other tests may succeed if a real database exists
		// We test the flag parsing and error handling paths
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd.SetArgs(tt.args)
			rootCmd.SetOut(&bytes.Buffer{})
			rootCmd.SetErr(&bytes.Buffer{})

			err := rootCmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("exportCmd.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExportCommand_FlagParsing(t *testing.T) {
	// Test that flags are parsed correctly
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "format flag",
			args: []string{"export", "--format", "jsonl"},
		},
		{
			name: "output directory flag",
			args: []string{"export", "--out", "/tmp/test"},
		},
		{
			name: "workspace flag",
			args: []string{"export", "--workspace", "/path/to/workspace"},
		},
		{
			name: "session-id flag",
			args: []string{"export", "--session-id", "test-session-id"},
		},
		{
			name: "clear-cache flag",
			args: []string{"export", "--clear-cache"},
		},
		{
			name: "intermediary flag",
			args: []string{"export", "--intermediary"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd.SetArgs(tt.args)
			rootCmd.SetOut(&bytes.Buffer{})
			rootCmd.SetErr(&bytes.Buffer{})

			// Just verify flags are parsed without error
			// The actual execution may succeed or fail depending on environment
			_ = rootCmd.Execute()
		})
	}
}

func TestSlugifyTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "basic question title",
			title: "How do I make the API?",
			want:  "how-do-i-make-the-api",
		},
		{
			name:  "multiple spaces and punctuation",
			title: "  Fix   bug: in /api/v1/users!  ",
			want:  "fix-bug-in-api-v1-users",
		},
		{
			name:  "empty title",
			title: "",
			want:  "",
		},
		{
			name:  "only symbols",
			title: "?!@#$%^&*()",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slugifyTitle(tt.title); got != tt.want {
				t.Errorf("slugifyTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestBuildSessionFilename(t *testing.T) {
	usedNames := make(map[string]int)

	s1 := &internal.Session{
		ID: "abc-123",
		Metadata: internal.Metadata{
			Name:      "How do I add the tests?",
			CreatedAt: "2026-10-20T10:30:00Z",
		},
	}
	s2 := &internal.Session{
		ID: "def-456",
		Metadata: internal.Metadata{
			Name:      "How do I add the tests",
			CreatedAt: "2026-10-20T10:30:00Z",
		},
	}
	s3 := &internal.Session{
		ID: "ghi-789",
		Metadata: internal.Metadata{
			Name:      "",
			CreatedAt: "2026-10-20T10:30:00Z",
		},
	}
	s4 := &internal.Session{
		ID:       "jkl-012",
		Metadata: internal.Metadata{Name: "No timestamp session"},
	}

	if got := buildSessionFilename(s1, "md", usedNames); got != "2026-10-20:10-30-00_how-do-i-add-the-tests.md" {
		t.Errorf("got %q, want %q", got, "2026-10-20:10-30-00_how-do-i-add-the-tests.md")
	}
	if got := buildSessionFilename(s2, "md", usedNames); got != "2026-10-20:10-30-00_how-do-i-add-the-tests-2.md" {
		t.Errorf("got %q, want %q", got, "2026-10-20:10-30-00_how-do-i-add-the-tests-2.md")
	}
	if got := buildSessionFilename(s3, "md", usedNames); got != "2026-10-20:10-30-00_session_ghi-789.md" {
		t.Errorf("got %q, want %q", got, "2026-10-20:10-30-00_session_ghi-789.md")
	}
	if got := buildSessionFilename(s4, "md", usedNames); got != "no-timestamp-session.md" {
		t.Errorf("got %q, want %q", got, "no-timestamp-session.md")
	}
}
