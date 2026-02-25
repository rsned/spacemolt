package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/rsned/spacemolt/pkg/credentials"
)

// command represents a benchmark command with its timing characteristics.
type command struct {
	name     string
	isMutation bool // tick-gated actions (undock, dock) need ~10s between calls
}

// commandSequence is the loop each agent runs repeatedly.
var commandSequence = []command{
	{name: "undock", isMutation: true},
	{name: "get_status"},
	{name: "get_poi"},
	{name: "get_system"},
	{name: "dock", isMutation: true},
	{name: "get_status"},
	{name: "get_base"},
}

// tickDelay is the minimum wait after a mutation command to let the server tick process it.
const tickDelay = 11 * time.Second

// Orchestrator manages agent lifecycle and command execution.
type Orchestrator struct {
	cfg     *Config
	metrics *Metrics
	limiter *AdaptiveRateLimiter
	creds   *credentials.FileProvider
	logger  *log.Logger
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(cfg *Config, metrics *Metrics, logger *log.Logger) *Orchestrator {
	return &Orchestrator{
		cfg:     cfg,
		metrics: metrics,
		limiter: NewAdaptiveRateLimiter(1 * time.Second),
		creds:   credentials.NewFileProvider(cfg.AgentsDir),
		logger:  logger,
	}
}

// Run executes the benchmark for the given backend type.
func (o *Orchestrator) Run(ctx context.Context, backendName string) error {
	agents, err := o.creds.ListAgents(ctx)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	if len(agents) == 0 {
		return fmt.Errorf("no agents found in %s", o.cfg.AgentsDir)
	}

	// Limit to requested agent count
	if len(agents) > o.cfg.Agents {
		agents = agents[:o.cfg.Agents]
	}

	o.logger.Printf("[%s] Starting benchmark with %d agents for %v", backendName, len(agents), o.cfg.Duration)

	// Calculate stagger interval
	stagger := time.Duration(0)
	if len(agents) > 1 {
		stagger = o.cfg.RampUp / time.Duration(len(agents)-1)
	}

	// Create a shared HTTP transport for HTTP/MCP multiplexing tests
	var sharedTransport *http.Transport
	if o.cfg.SharedConn {
		sharedTransport = &http.Transport{
			MaxIdleConns:        o.cfg.PoolSize,
			MaxIdleConnsPerHost: o.cfg.PoolSize,
			IdleConnTimeout:     90 * time.Second,
		}
	}

	// Launch agents with staggered starts
	for i, agentID := range agents {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Stagger start
		if i > 0 && stagger > 0 {
			select {
			case <-time.After(stagger):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		go o.runAgent(ctx, backendName, agentID, sharedTransport)
	}

	o.logger.Printf("[%s] All %d agents launched, running for %v", backendName, len(agents), o.cfg.Duration)

	// Reset metrics start time so duration/throughput reflect the actual test window
	o.metrics.ResetStart()

	// Wait for the test duration to elapse
	select {
	case <-time.After(o.cfg.Duration):
	case <-ctx.Done():
	}

	return nil
}

func (o *Orchestrator) runAgent(ctx context.Context, backendName, agentID string, sharedTransport *http.Transport) {
	cred, err := o.creds.GetCredentials(ctx, agentID)
	if err != nil {
		o.logger.Printf("[%s] %s: failed to load credentials: %v", backendName, agentID, err)
		return
	}

	backend, err := o.createBackend(backendName, sharedTransport)
	if err != nil {
		o.logger.Printf("[%s] %s: failed to create backend: %v", backendName, agentID, err)
		return
	}
	defer func() { _ = backend.Close() }()

	// Connect
	if err := backend.Connect(ctx); err != nil {
		o.logger.Printf("[%s] %s: connect failed: %v", backendName, agentID, err)
		o.metrics.Record(CommandResult{
			Backend:   backendName,
			AgentID:   agentID,
			Command:   "connect",
			Err:       err,
			ErrorKind: TransportError,
			Timestamp: time.Now(),
		})
		return
	}

	// Login
	start := time.Now()
	if err := backend.Login(ctx, cred.Username, cred.Password); err != nil {
		loginKind, loginCode := classifyError(err, nil)
		o.logger.Printf("[%s] %s: login failed: %v", backendName, agentID, err)
		o.metrics.Record(CommandResult{
			Backend:   backendName,
			AgentID:   agentID,
			Command:   "login",
			Latency:   time.Since(start),
			Err:       err,
			ErrorKind: loginKind,
			ErrorCode: loginCode,
			Timestamp: time.Now(),
		})
		return
	}
	o.metrics.Record(CommandResult{
		Backend:   backendName,
		AgentID:   agentID,
		Command:   "login",
		Latency:   time.Since(start),
		Timestamp: time.Now(),
	})

	o.logger.Printf("[%s] %s: connected and logged in", backendName, agentID)

	// Command loop
	for ctx.Err() == nil {
		for _, cmd := range commandSequence {
			if ctx.Err() != nil {
				return
			}

			// Rate limit wait
			if err := o.limiter.Wait(ctx); err != nil {
				return
			}

			start := time.Now()
			resp, err := backend.SendCommand(ctx, cmd.name, nil)
			latency := time.Since(start)

			kind, errCode := classifyError(err, resp)

			result := CommandResult{
				Backend:   backendName,
				AgentID:   agentID,
				Command:   cmd.name,
				Latency:   latency,
				Err:       err,
				ErrorKind: kind,
				ErrorCode: errCode,
				Timestamp: time.Now(),
			}
			o.metrics.Record(result)

			if kind == RateLimitError {
				o.limiter.RecordHit()
			} else if err == nil {
				o.limiter.RecordSuccess()
			}

			if o.cfg.Verbose && err != nil {
				o.logger.Printf("[%s] %s: %s error (%v): %v", backendName, agentID, cmd.name, latency, err)
			}

			// Mutation commands are tick-gated (~10s), wait for the tick to process
			// before sending the next command. Queries use the normal delay.
			delay := o.cfg.Delay
			if cmd.isMutation {
				delay = tickDelay
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
	}
}

func (o *Orchestrator) createBackend(name string, sharedTransport *http.Transport) (Backend, error) {
	switch name {
	case "ws":
		return NewWSBackend(o.cfg.ServerURL, o.metrics), nil
	case "http":
		return NewHTTPBackend(o.cfg.httpBaseURL(), sharedTransport), nil
	case "mcp":
		return NewMCPBackend(o.cfg.httpBaseURL(), sharedTransport), nil
	default:
		return nil, fmt.Errorf("unknown backend: %s", name)
	}
}
