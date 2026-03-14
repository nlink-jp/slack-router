package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Slack  SlackConfig   `yaml:"slack"`
	Global GlobalConfig  `yaml:"global"`
	Routes []RouteConfig `yaml:"routes"`
}

type SlackConfig struct {
	AppToken string `yaml:"app_token"`
	BotToken string `yaml:"bot_token"`
}

type GlobalConfig struct {
	MaxConcurrentWorkers    int            `yaml:"max_concurrent_workers"`
	LogLevel                string         `yaml:"log_level"`
	HeartbeatIntervalStr    string         `yaml:"heartbeat_interval"`
	Messages                GlobalMessages `yaml:"messages"`

	// HeartbeatInterval is parsed from HeartbeatIntervalStr. 0 disables heartbeat logging.
	HeartbeatInterval time.Duration `yaml:"-"`
}

// GlobalMessages holds user-facing notification strings that apply server-wide.
// All fields have built-in defaults and can be omitted from config.yaml.
type GlobalMessages struct {
	// ServerBusy is shown when the global concurrency limit is reached.
	ServerBusy string `yaml:"server_busy"`
}

type RouteConfig struct {
	Command        string `yaml:"command"`
	Script         string `yaml:"script"`
	TimeoutStr     string `yaml:"timeout"`
	MaxConcurrency int    `yaml:"max_concurrency"`

	// BusyMessage is shown when this route's concurrency limit is reached.
	BusyMessage string `yaml:"busy_message"`
	// DenyMessage is shown when an ACL rule rejects the request.
	DenyMessage string `yaml:"deny_message"`
	// ErrorMessage is shown when the router fails to start the worker process.
	ErrorMessage string `yaml:"error_message"`

	// ACL fields are inlined so they appear at the same level in YAML.
	ACL `yaml:",inline"`

	// parsed from TimeoutStr
	Timeout time.Duration `yaml:"-"`
}

func LoadConfig(path string) (*Config, error) {
	// Resolve the config path itself to absolute so that script paths
	// derived from it are also absolute and CWD-independent.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving config path %q: %w", path, err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", absPath, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	configDir := filepath.Dir(absPath)
	if err := cfg.validate(configDir); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate(configDir string) error {
	// Tokens may come from environment variables instead of (or in addition to)
	// the config file. Environment variables take precedence, so operators can
	// keep config.yaml token-free and inject secrets at runtime.
	if v := os.Getenv("SLACK_APP_TOKEN"); v != "" {
		c.Slack.AppToken = v
	}
	if v := os.Getenv("SLACK_BOT_TOKEN"); v != "" {
		c.Slack.BotToken = v
	}

	if c.Slack.AppToken == "" {
		return fmt.Errorf("slack app token is not set: provide SLACK_APP_TOKEN env var or slack.app_token in config")
	}
	if c.Slack.BotToken == "" {
		return fmt.Errorf("slack bot token is not set: provide SLACK_BOT_TOKEN env var or slack.bot_token in config")
	}
	if c.Global.MaxConcurrentWorkers <= 0 {
		c.Global.MaxConcurrentWorkers = 10
	}
	if c.Global.LogLevel == "" {
		c.Global.LogLevel = "info"
	}
	if c.Global.Messages.ServerBusy == "" {
		c.Global.Messages.ServerBusy = ":warning: サーバーが混み合っています。しばらく待ってから再度お試しください。"
	}
	if c.Global.HeartbeatIntervalStr != "" {
		d, err := time.ParseDuration(c.Global.HeartbeatIntervalStr)
		if err != nil {
			return fmt.Errorf("global.heartbeat_interval: invalid duration %q: %w", c.Global.HeartbeatIntervalStr, err)
		}
		if d < 0 {
			return fmt.Errorf("global.heartbeat_interval must be >= 0")
		}
		c.Global.HeartbeatInterval = d
	} else {
		c.Global.HeartbeatInterval = 1 * time.Minute
	}

	seen := make(map[string]struct{})
	for i := range c.Routes {
		r := &c.Routes[i]
		if r.Command == "" {
			return fmt.Errorf("routes[%d]: command is required", i)
		}
		if r.Script == "" {
			return fmt.Errorf("route %q: script is required", r.Command)
		}
		if _, dup := seen[r.Command]; dup {
			return fmt.Errorf("route %q: duplicate command", r.Command)
		}
		seen[r.Command] = struct{}{}

		// Resolve script path to absolute, anchored at the config file's
		// directory — not the process CWD. This makes behaviour identical
		// regardless of where slack-router is invoked from.
		if !filepath.IsAbs(r.Script) {
			r.Script = filepath.Join(configDir, r.Script)
		}
		r.Script = filepath.Clean(r.Script)

		if err := validateScript(r.Command, r.Script); err != nil {
			return err
		}

		if r.TimeoutStr != "" {
			d, err := time.ParseDuration(r.TimeoutStr)
			if err != nil {
				return fmt.Errorf("route %q: invalid timeout %q: %w", r.Command, r.TimeoutStr, err)
			}
			r.Timeout = d
		} else {
			r.Timeout = 5 * time.Minute // default
		}

		if r.MaxConcurrency < 0 {
			return fmt.Errorf("route %q: max_concurrency must be >= 0", r.Command)
		}
		if r.BusyMessage == "" {
			r.BusyMessage = ":warning: このコマンドは現在処理中です。しばらく待ってから再度お試しください。"
		}
		if r.DenyMessage == "" {
			r.DenyMessage = ":no_entry: このコマンドを実行する権限がありません。"
		}
		if r.ErrorMessage == "" {
			r.ErrorMessage = ":x: コマンドの実行を開始できませんでした。しばらく待ってから再度お試しください。"
		}
	}

	return nil
}

// validateScript checks that the script at path is safe to execute.
func validateScript(command, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("route %q: script %q: %w", command, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("route %q: script %q is a directory", command, path)
	}

	mode := info.Mode()

	// Must be executable by the owner.
	if mode&0100 == 0 {
		return fmt.Errorf("route %q: script %q is not executable (run: chmod +x %s)", command, path, path)
	}

	// Refuse world-writable scripts — anyone on the system could tamper with them.
	if mode&0002 != 0 {
		return fmt.Errorf("route %q: script %q is world-writable, which is a security risk (run: chmod o-w %s)", command, path, path)
	}

	return nil
}
