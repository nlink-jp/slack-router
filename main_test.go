package main

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestVersionFlag builds the binary the way the Makefile does (-X main.version
// and friends) and runs it the way make verify-release does. It runs in an
// empty directory, so a --version that loaded config.yaml first would fail.
func TestVersionFlag(t *testing.T) {
	const (
		wantVersion = "v9.8.7-test"
		wantCommit  = "abc1234"
		wantDate    = "2026-09-23T00:00:00Z"
	)
	bin := filepath.Join(t.TempDir(), "slack-router")
	ldflags := "-X main.version=" + wantVersion +
		" -X main.commit=" + wantCommit +
		" -X main.buildDate=" + wantDate
	build := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	want := "slack-router " + wantVersion + " (commit " + wantCommit + ", built " + wantDate + ")\n"
	for _, arg := range []string{"--version", "-version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(bin, arg)
			cmd.Dir = t.TempDir()
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("%s: %v (want exit 0)\nstderr: %s", arg, err, stderr.String())
			}
			if stdout.String() != want {
				t.Errorf("%s stdout = %q, want %q", arg, stdout.String(), want)
			}
			if stderr.Len() != 0 {
				t.Errorf("%s stderr = %q, want empty", arg, stderr.String())
			}
		})
	}
}

func TestVersionLine(t *testing.T) {
	oldV, oldC, oldD := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = oldV, oldC, oldD })

	version, commit, buildDate = "v1.2.3", "deadbee", "2026-01-02T03:04:05Z"
	if got, want := versionLine(), "slack-router v1.2.3 (commit deadbee, built 2026-01-02T03:04:05Z)"; got != want {
		t.Errorf("versionLine() = %q, want %q", got, want)
	}
}

func TestStartHeartbeatEmitsLog(t *testing.T) {
	h := &captureHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startHeartbeat(ctx, 20*time.Millisecond, time.Now())

	// Wait long enough for at least one tick.
	time.Sleep(60 * time.Millisecond)

	if !h.findLog("heartbeat", "uptime", "") {
		t.Error("expected at least one heartbeat log entry")
	}
}

func TestStartHeartbeatDisabledWhenZero(t *testing.T) {
	h := &captureHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startHeartbeat(ctx, 0, time.Now())
	time.Sleep(30 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == "heartbeat" {
			t.Error("expected no heartbeat log when interval is 0")
		}
	}
}

func TestStartHeartbeatStopsOnContextCancel(t *testing.T) {
	h := &captureHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })

	ctx, cancel := context.WithCancel(context.Background())
	startHeartbeat(ctx, 20*time.Millisecond, time.Now())

	// Let one tick fire, then cancel.
	time.Sleep(40 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)

	h.mu.Lock()
	countBefore := 0
	for _, r := range h.records {
		if r.Message == "heartbeat" {
			countBefore++
		}
	}
	h.mu.Unlock()

	// Wait well past another tick interval; count must not increase.
	time.Sleep(50 * time.Millisecond)

	h.mu.Lock()
	countAfter := 0
	for _, r := range h.records {
		if r.Message == "heartbeat" {
			countAfter++
		}
	}
	h.mu.Unlock()

	if countAfter > countBefore {
		t.Errorf("heartbeat continued after context cancel: before=%d after=%d", countBefore, countAfter)
	}
}
