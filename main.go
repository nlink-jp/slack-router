package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// Build-time variables injected via -ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	// Structured JSON logger; level controlled by config.
	logLevel := parseLogLevel(cfg.Global.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	router := NewRouter(cfg)

	api := slack.New(
		cfg.Slack.BotToken,
		slack.OptionAppLevelToken(cfg.Slack.AppToken),
	)
	smClient := socketmode.New(api)

	// ctx is cancelled on SIGINT/SIGTERM; this triggers graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startTime := time.Now()

	slog.Info("slack-router starting",
		"version", version,
		"commit", commit,
		"build_date", buildDate,
		"routes", len(cfg.Routes),
		"max_concurrent_workers", cfg.Global.MaxConcurrentWorkers,
		"heartbeat_interval", cfg.Global.HeartbeatInterval.String(),
	)

	startHeartbeat(ctx, cfg.Global.HeartbeatInterval, startTime)

	// Event dispatch loop.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return

			case evt, ok := <-smClient.Events:
				if !ok {
					return
				}
				handleEvent(ctx, smClient, router, evt)
			}
		}
	}()

	// RunContext blocks until the context is cancelled or a fatal error occurs.
	if err := smClient.RunContext(ctx); err != nil && ctx.Err() == nil {
		slog.Error("socket mode error", "err", err)
		os.Exit(1)
	}

	slog.Info("shutting down: waiting for in-flight workers to finish")
	router.Wait()
	slog.Info("shutdown complete")
}

func handleEvent(ctx context.Context, client *socketmode.Client, router *Router, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		slog.Info("connecting to slack")

	case socketmode.EventTypeConnected:
		slog.Info("connected to slack")

	case socketmode.EventTypeDisconnect:
		slog.Warn("disconnected from slack")

	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			slog.Error("unexpected data type for slash command event")
			return
		}

		// ACK immediately — Slack requires a response within 3 seconds.
		client.Ack(*evt.Request)

		event := SlashEvent{
			Command:     cmd.Command,
			Text:        cmd.Text,
			UserID:      cmd.UserID,
			ChannelID:   cmd.ChannelID,
			ResponseURL: cmd.ResponseURL,
		}

		// cmd.Text is intentionally omitted from the log to avoid
		// recording potentially sensitive user input (passwords, tokens, etc.).
		slog.Info("slash command received",
			"command", cmd.Command,
			"user", cmd.UserID,
			"channel", cmd.ChannelID,
		)

		if err := router.Dispatch(ctx, event); err != nil {
			var de *DispatchError
			if errors.As(err, &de) {
				slog.Warn("request dropped", "command", cmd.Command, "user", cmd.UserID, "reason", de.Reason)
				responseURL := cmd.ResponseURL
				message := de.Message
				router.GoNotify(func() { notifyEphemeral(responseURL, message) })
			}
		}

	default:
		slog.Debug("unhandled event type", "type", evt.Type)
	}
}

// startHeartbeat emits a periodic "heartbeat" log entry for health monitoring.
// Log aggregation systems can alert when heartbeat stops appearing.
// interval=0 disables heartbeat logging entirely.
func startHeartbeat(ctx context.Context, interval time.Duration, startTime time.Time) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				slog.Info("heartbeat", "uptime", time.Since(startTime).Round(time.Second).String())
			}
		}
	}()
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
