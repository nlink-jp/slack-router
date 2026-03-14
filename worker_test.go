package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureHandler is a minimal slog.Handler that records log entries for inspection in tests.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler          { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler               { return h }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

// findLog returns the first log record with msg matching any attr key=value pair.
func (h *captureHandler) findLog(msg, attrKey, attrContains string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message != msg {
			continue
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == attrKey && strings.Contains(a.Value.String(), attrContains) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

func TestValidateResponseURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"empty url", "", true},
		{"valid slack url", "https://hooks.slack.com/commands/T123/456/abc", false},
		{"http scheme rejected", "http://hooks.slack.com/commands/T123/456/abc", true},
		{"wrong host", "https://hooks.example.com/commands/T123/456/abc", true},
		{"wrong host - slack.com only", "https://slack.com/commands/T123/456/abc", true},
		{"ftp scheme", "ftp://hooks.slack.com/commands/T123/456/abc", true},
		{"no scheme", "hooks.slack.com/commands/T123/456/abc", true},
		{"file scheme", "file:///etc/passwd", true},
		{"internal address", "https://169.254.169.254/latest/meta-data/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResponseURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResponseURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestSanitizedEnv(t *testing.T) {
	t.Setenv("SLACK_APP_TOKEN", "xapp-test-token")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test-token")
	t.Setenv("HOME", "/home/testuser")

	result := sanitizedEnv()

	for _, kv := range result {
		key, _, _ := strings.Cut(kv, "=")
		if key == "SLACK_APP_TOKEN" {
			t.Error("sanitizedEnv: SLACK_APP_TOKEN must not appear in output")
		}
		if key == "SLACK_BOT_TOKEN" {
			t.Error("sanitizedEnv: SLACK_BOT_TOKEN must not appear in output")
		}
	}

	// Non-sensitive vars must be preserved.
	found := false
	for _, kv := range result {
		if strings.HasPrefix(kv, "HOME=") {
			found = true
			break
		}
	}
	if !found {
		t.Error("sanitizedEnv: HOME should be preserved in output")
	}
}

func TestRunWorkerLogsOutput(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "worker.sh")
	err := os.WriteFile(script, []byte("#!/bin/sh\necho 'hello stdout'\necho 'hello stderr' >&2\n"), 0o755)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := &captureHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })

	runWorker(context.Background(), script, 5*time.Second, SlashEvent{
		Command: "/test", UserID: "U001", ChannelID: "C001",
	})

	if !h.findLog("worker stdout", "output", "hello stdout") {
		t.Error("expected stdout to be logged under 'worker stdout'")
	}
	if !h.findLog("worker stderr", "output", "hello stderr") {
		t.Error("expected stderr to be logged under 'worker stderr'")
	}
}

func TestRunWorkerNoOutputWhenSilent(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "silent.sh")
	err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := &captureHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })

	runWorker(context.Background(), script, 5*time.Second, SlashEvent{
		Command: "/test", UserID: "U001", ChannelID: "C001",
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == "worker stdout" || r.Message == "worker stderr" {
			t.Errorf("expected no output logs for silent script, got message: %q", r.Message)
		}
	}
}

func TestSanitizedEnvNoSensitiveKeys(t *testing.T) {
	// Ensure no sensitive keys leak even if the env has only those vars.
	os.Unsetenv("SLACK_APP_TOKEN")
	os.Unsetenv("SLACK_BOT_TOKEN")

	result := sanitizedEnv()
	for _, kv := range result {
		key, _, _ := strings.Cut(kv, "=")
		if _, blocked := sensitiveEnvKeys[key]; blocked {
			t.Errorf("sanitizedEnv: sensitive key %q found in output", key)
		}
	}
}
