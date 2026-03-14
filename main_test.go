package main

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

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
