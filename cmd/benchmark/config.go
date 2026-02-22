package main

import (
	"flag"
	"fmt"
	"time"
)

// Config holds all benchmark configuration.
type Config struct {
	Backend    string
	Agents     int
	AgentsDir  string
	Duration   time.Duration
	RampUp     time.Duration
	Delay      time.Duration
	SharedConn bool
	PoolSize   int
	Output     string
	OutputFile string
	Verbose    bool
	ServerURL  string
}

func parseConfig() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.Backend, "backend", "all", "Backend to test: ws, http, mcp, or all")
	flag.IntVar(&cfg.Agents, "agents", 10, "Number of agents to simulate")
	flag.StringVar(&cfg.AgentsDir, "agents-dir", "data/agents", "Directory containing agent credentials")
	flag.DurationVar(&cfg.Duration, "duration", 5*time.Minute, "Test duration")
	flag.DurationVar(&cfg.RampUp, "ramp-up", 30*time.Second, "Stagger agent starts over this duration")
	flag.DurationVar(&cfg.Delay, "delay", 500*time.Millisecond, "Delay between commands per agent")
	flag.BoolVar(&cfg.SharedConn, "shared-conn", false, "Test multiplexed/shared connections")
	flag.IntVar(&cfg.PoolSize, "pool-size", 4, "Connection pool size for shared mode")
	flag.StringVar(&cfg.Output, "output", "text", "Output format: text, json, csv")
	flag.StringVar(&cfg.OutputFile, "output-file", "", "Export path (empty = stdout only)")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Per-command output")
	flag.StringVar(&cfg.ServerURL, "server", "wss://game.spacemolt.com/ws", "Game server WebSocket URL")

	flag.Parse()

	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	switch c.Backend {
	case "ws", "http", "mcp", "all":
	default:
		return fmt.Errorf("invalid backend %q: must be ws, http, mcp, or all", c.Backend)
	}

	if c.Agents < 1 {
		return fmt.Errorf("agents must be >= 1, got %d", c.Agents)
	}

	if c.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}

	switch c.Output {
	case "text", "json", "csv":
	default:
		return fmt.Errorf("invalid output format %q: must be text, json, or csv", c.Output)
	}

	if c.PoolSize < 1 {
		return fmt.Errorf("pool-size must be >= 1, got %d", c.PoolSize)
	}

	return nil
}

// httpBaseURL derives the HTTP API base URL from the WebSocket URL.
func (c *Config) httpBaseURL() string {
	// wss://game.spacemolt.com/ws → https://game.spacemolt.com
	url := c.ServerURL
	if len(url) > 6 && url[:6] == "wss://" {
		url = "https://" + url[6:]
	} else if len(url) > 5 && url[:5] == "ws://" {
		url = "http://" + url[5:]
	}
	// Strip trailing /ws path
	if len(url) > 3 && url[len(url)-3:] == "/ws" {
		url = url[:len(url)-3]
	}
	return url
}

// backends returns the list of backend names to test.
func (c *Config) backends() []string {
	if c.Backend == "all" {
		return []string{"ws", "http", "mcp"}
	}
	return []string{c.Backend}
}
