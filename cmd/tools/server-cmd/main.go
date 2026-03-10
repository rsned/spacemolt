// server-cmd sends a raw command to the game server using an agent's credentials
// and prints the full JSON response.
//
// Usage:
//
//	server-cmd --agent=pirate-1 --cmd=get_status
//	server-cmd --agent=miner-1 --cmd=mine --payload target=asteroid-123
//	server-cmd --agent=trader-1 --cmd=buy --payload item=fuel --payload quantity=10
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
)

const gameServerURL = "wss://game.spacemolt.com/ws"

// payloadFlags collects multiple --payload key=value flags.
type payloadFlags []string

func (p *payloadFlags) String() string { return strings.Join(*p, ", ") }
func (p *payloadFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}

// responseHandler captures all responses after the command is sent.
type responseHandler struct {
	cmdSent  bool
	mu       sync.Mutex
	resultCh chan protocol.Response
}

func (h *responseHandler) OnConnected(_ *game.State) {}
func (h *responseHandler) OnDisconnected(_ error)    {}

func (h *responseHandler) OnMessage(resp protocol.Response) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.cmdSent {
		return
	}
	// Forward all response types to the collector.
	select {
	case h.resultCh <- resp:
	default:
	}
}

func (h *responseHandler) markSent() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cmdSent = true
}

func main() {
	agentID := flag.String("agent", "", "Agent ID (e.g. pirate-1, miner-1)")
	cmd := flag.String("cmd", "", "Server command to send (e.g. get_status, mine, buy)")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	var payload payloadFlags
	flag.Var(&payload, "payload", "Payload key=value pair (repeatable)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: server-cmd --agent=<id> --cmd=<command> [--payload key=value ...]\n\n")
		fmt.Fprintf(os.Stderr, "Send a raw command to the game server and print the JSON response.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  server-cmd --agent=pirate-1 --cmd=get_status\n")
		fmt.Fprintf(os.Stderr, "  server-cmd --agent=miner-1 --cmd=mine --payload target=asteroid-123\n")
		fmt.Fprintf(os.Stderr, "  server-cmd --agent=trader-1 --cmd=buy --payload item=fuel --payload quantity=10\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *agentID == "" || *cmd == "" {
		flag.Usage()
		os.Exit(1)
	}

	if *transport != "ws" {
		fmt.Fprintf(os.Stderr, "Error: server-cmd only supports --transport=ws (raw command protocol requires WebSocket)\n")
		os.Exit(1)
	}

	// Build payload map from key=value pairs.
	payloadMap := make(map[string]any)
	for _, kv := range payload {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: invalid payload format %q (expected key=value)\n", kv)
			os.Exit(1)
		}
		// Try to parse as number or boolean for convenience.
		payloadMap[key] = parseValue(value)
	}

	// Load credentials.
	provider := credentials.NewFileProvider("data/agents")
	ctx := context.Background()
	creds, err := provider.GetCredentials(ctx, *agentID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading credentials for %q: %v\n", *agentID, err)
		os.Exit(1)
	}

	// Connect to game server.
	logger := log.New(os.Stderr, fmt.Sprintf("[%s] ", *agentID), log.LstdFlags)
	client := game.NewClient(gameServerURL, creds.Username, creds.Password, logger)
	client.SetDebugLogging(*debug)

	handler := &responseHandler{
		resultCh: make(chan protocol.Response, 64),
	}
	client.SetHandler(handler)

	if err := client.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	<-client.Ready()
	time.Sleep(game.SleepRetry)

	if err := client.Login(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error logging in: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(game.SleepQuick)

	// Mark handler ready to capture, then send the command.
	handler.markSent()
	if err := client.Send(ctx, protocol.Message{
		Type:      *cmd,
		Payload:   payloadMap,
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error sending command: %v\n", err)
		os.Exit(1)
	}

	// Collect all response messages. Wait up to SleepTick for the first,
	// then keep collecting until no new messages arrive for SleepQuick.
	var responses []protocol.Response
	timeout := time.After(game.SleepTick)
	for {
		quietPeriod := game.SleepTick + game.SleepQuick
		if len(responses) == 0 {
			// First message: use the full timeout.
			select {
			case resp := <-handler.resultCh:
				responses = append(responses, resp)
				continue
			case <-timeout:
				fmt.Fprintln(os.Stderr, "Error: timed out waiting for response")
				os.Exit(1)
			}
		}
		// Subsequent messages: wait a short quiet period for more.
		select {
		case resp := <-handler.resultCh:
			responses = append(responses, resp)
		case <-time.After(quietPeriod):
			// No more messages; done collecting.
			goto done
		}
	}
done:
	for i, resp := range responses {
		if i > 0 {
			fmt.Println("---")
		}
		output := map[string]any{
			"type":    resp.Type,
			"payload": resp.Payload,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling response: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(data))
	}
}

// parseValue attempts to parse a string as a number or boolean,
// falling back to string.
func parseValue(s string) any {
	// Boolean
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	}

	// Integer
	var i int64
	if _, err := fmt.Sscanf(s, "%d", &i); err == nil && fmt.Sprintf("%d", i) == s {
		return i
	}

	// Float
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
		return f
	}

	return s
}
