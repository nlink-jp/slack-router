package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// SlashEvent is the JSON payload written to a worker's stdin.
type SlashEvent struct {
	Command     string `json:"command"`
	Text        string `json:"text"`
	UserID      string `json:"user_id"`
	ChannelID   string `json:"channel_id"`
	ResponseURL string `json:"response_url"`
}

// notifyHTTPClient is shared across notifyEphemeral calls.
// The 5-second timeout prevents goroutine hangs on slow or unresponsive endpoints.
var notifyHTTPClient = &http.Client{Timeout: 5 * time.Second}

// runWorker starts script as a child process, writes event as JSON to its
// stdin, then waits for it to exit.
//
// On timeout the worker receives SIGTERM; if it does not exit within
// gracePeriod it receives SIGKILL.
func runWorker(ctx context.Context, script string, timeout time.Duration, event SlashEvent) {
	workerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(script) //nolint:gosec // script path comes from config, not user input
	// Place the child in its own process group so SIGTERM/SIGKILL hits
	// the whole subtree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Strip sensitive router credentials from the child's environment.
	// Workers receive all other env vars (PATH, HOME, …) unchanged.
	cmd.Env = sanitizedEnv()

	// Capture stdout and stderr so worker output appears in the router's
	// structured log. Without this, output would be silently discarded when
	// the process runs as a daemon (no controlling terminal).
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	stdin, err := cmd.StdinPipe()
	if err != nil {
		slog.Error("worker: stdin pipe failed", "command", event.Command, "script", script, "err", err)
		return
	}

	if err := cmd.Start(); err != nil {
		stdin.Close() // prevent pipe fd leak when Start fails
		slog.Error("worker: start failed", "command", event.Command, "script", script, "err", err)
		return
	}

	pid := cmd.Process.Pid
	slog.Info("worker started", "pid", pid, "command", event.Command, "script", script, "user", event.UserID)

	// Write JSON payload to stdin then close.
	// If encoding fails the worker would run without its input data, so we
	// kill it immediately to avoid silent misbehaviour.
	go func() {
		defer stdin.Close()
		enc := json.NewEncoder(stdin)
		if err := enc.Encode(event); err != nil {
			slog.Warn("worker: stdin write error, killing process", "pid", pid, "err", err)
			cmd.Process.Kill() //nolint:errcheck
		}
	}()

	// Collect the process exit asynchronously.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// logOutput emits any captured stdout/stderr to the structured log.
	// Call this after <-done confirms the process has exited and all output
	// has been flushed to the buffers.
	logOutput := func() {
		if out := strings.TrimSpace(stdoutBuf.String()); out != "" {
			slog.Info("worker stdout", "pid", pid, "command", event.Command, "output", out)
		}
		if out := strings.TrimSpace(stderrBuf.String()); out != "" {
			slog.Warn("worker stderr", "pid", pid, "command", event.Command, "output", out)
		}
	}

	select {
	case err := <-done:
		logOutput()
		if err != nil {
			slog.Error("worker abnormal exit", "pid", pid, "command", event.Command, "err", err)
		} else {
			slog.Info("worker exited normally", "pid", pid, "command", event.Command)
		}

	case <-workerCtx.Done():
		// Timeout hit (or parent context cancelled).
		reason := "timeout"
		if ctx.Err() != nil {
			reason = "shutdown"
		}
		slog.Warn("worker: sending SIGTERM", "pid", pid, "command", event.Command, "reason", reason)
		_ = syscall.Kill(-pid, syscall.SIGTERM)

		select {
		case <-done:
			logOutput()
			slog.Info("worker: exited after SIGTERM", "pid", pid)
		case <-time.After(5 * time.Second):
			slog.Warn("worker: sending SIGKILL", "pid", pid)
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			<-done
			logOutput()
			slog.Warn("worker: killed", "pid", pid)
		}
	}
}

// slackResponse is the payload sent to a Slack response_url.
type slackResponse struct {
	ResponseType string `json:"response_type"`
	Text         string `json:"text"`
}

// notifyEphemeral posts a message visible only to the requesting user.
// response_type "ephemeral" ensures the message is not shown to the channel.
//
// The function validates responseURL against Slack's known webhook domain to
// prevent SSRF. A 5-second HTTP timeout is enforced via notifyHTTPClient.
func notifyEphemeral(responseURL, message string) {
	if err := validateResponseURL(responseURL); err != nil {
		slog.Warn("notifyEphemeral: skipping invalid response_url", "err", err)
		return
	}

	body, _ := json.Marshal(slackResponse{
		ResponseType: "ephemeral",
		Text:         message,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, responseURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("notifyEphemeral: failed to build request", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := notifyHTTPClient.Do(req)
	if err != nil {
		slog.Warn("notifyEphemeral: http post failed", "err", err)
		return
	}
	resp.Body.Close()
}

// sensitiveEnvKeys lists environment variables that must not be passed to
// worker scripts. These are credentials held by the router process itself.
var sensitiveEnvKeys = map[string]struct{}{
	"SLACK_APP_TOKEN": {},
	"SLACK_BOT_TOKEN": {},
}

// sanitizedEnv returns a copy of the current process environment with
// sensitive credentials removed. Workers inherit PATH, HOME, and all other
// general-purpose variables, but never the router's Slack tokens.
func sanitizedEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		key, _, _ := strings.Cut(kv, "=")
		if _, blocked := sensitiveEnvKeys[key]; !blocked {
			out = append(out, kv)
		}
	}
	return out
}

// validateResponseURL ensures the URL is an HTTPS endpoint on hooks.slack.com,
// preventing SSRF attacks via a manipulated response_url.
func validateResponseURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("response_url is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("response_url parse error: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("response_url must use https, got %q", u.Scheme)
	}
	if u.Host != "hooks.slack.com" {
		return fmt.Errorf("response_url host must be hooks.slack.com, got %q", u.Host)
	}
	return nil
}
