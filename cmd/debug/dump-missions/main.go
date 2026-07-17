// Command dump-missions logs in as an agent, fetches the mission board at the
// current station plus the agent's active missions, and prints the raw server
// JSON for both. Built for the mission-runner live smoke: the selector's
// deliver-shape filter depends on the exact board-entry shape (objectives vs
// the legacy requirements block), and only the wire payload settles it.
//
// Usage: go run ./cmd/debug/dump-missions --agent engineer-1
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

const defaultURL = "wss://game.spacemolt.com/ws"

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func loadCreds(account string) (credentials, error) {
	path := filepath.Join("data", "agents", account, "credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return credentials{}, fmt.Errorf("read %s: %w", path, err)
	}
	var c credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return credentials{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Username == "" || c.Password == "" {
		return credentials{}, fmt.Errorf("%s: missing username/password", path)
	}
	return c, nil
}

func pretty(raw []byte) string {
	var buf map[string]any
	if err := json.Unmarshal(raw, &buf); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(buf, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func main() {
	agent := flag.String("agent", "", "Agent ID (required, e.g. engineer-1)")
	flag.Parse()
	if *agent == "" {
		flag.PrintDefaults()
		os.Exit(2)
	}

	creds, err := loadCreds(*agent)
	if err != nil {
		log.Fatal(err)
	}

	gameLog := log.New(io.Discard, "", 0)
	client := game.NewClient(defaultURL, creds.Username, creds.Password, gameLog)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer func() { _ = client.Disconnect() }()
	select {
	case <-client.Ready():
	case <-ctx.Done():
		log.Fatal("timed out waiting for ready")
	}
	if err := client.Login(ctx); err != nil {
		log.Fatalf("login: %v", err)
	}

	st := client.GetState()
	fmt.Printf("=== %s @ %s / %s (docked=%v) ===\n", *agent, st.System.ID, st.CurrentPOI, st.Doc)

	if err := client.GetMissions(ctx); err != nil {
		log.Printf("get_missions: %v", err)
	}
	time.Sleep(game.SleepQuick)
	fmt.Println("=== RAW get_missions ===")
	fmt.Println(pretty(client.GetRawJSON("missions")))

	if err := client.GetActiveMissions(ctx); err != nil {
		log.Printf("get_active_missions: %v", err)
	}
	time.Sleep(game.SleepQuick)
	fmt.Println("=== RAW get_active_missions ===")
	fmt.Println(pretty(client.GetRawJSON("active_missions")))
}
