// Command login-probe empirically measures the game server's per-IP /login
// rate limit so launch pacing (now the host-wide connection gate; formerly
// the supervisor StaggerInterval) can be validated against the real ceiling.
//
// It runs from the SAME host/IP as the fleet, so tripping the limit will
// briefly lock out this IP (the server returns code="ip_timed_out" with a
// block duration; the client also adds up to game.SleepIPRateLimitJitter).
// Run it only while the fleet is down.
//
// Modes:
//
//	burst  - log in as fast as possible (fresh connection each), stop at the
//	         first rate-limit, and report how many logins succeeded before the
//	         block plus the reported block duration. Capped by --max.
//	paced  - log in once per --interval for --duration. If no block occurs the
//	         pace is safe; the first block stops the run and flags the pace as
//	         too aggressive. Default --interval is game.SleepMedium (5s).
//	clerk  - log in --count times using Clerk ws-tokens instead of the password.
//	         One token is minted per login (a ws-token is single-use), then a
//	         fresh connection is opened and LoginWithToken is called. Logins are
//	         spaced by --interval and the run stops at the first error. Requires
//	         the agent's credentials.json to carry a player_id (run clerk-sync)
//	         and a Clerk key in SPACEMOLT_CLERK_API_KEY or the key file. Neither
//	         the key nor any token is printed.
//
// Each login uses a fresh connection because login is a once-per-connection
// operation. The probe stops the instant it sees a rate-limit — it never keeps
// hammering a blocked IP.
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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/clerk"
)

const defaultURL = "wss://game.spacemolt.com/ws"

// connectTimeout bounds a single connect+ready+login attempt.
const connectTimeout = 15 * time.Second

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
}

type outcomeKind int

const (
	outcomeSuccess outcomeKind = iota
	outcomeRateLimited
	outcomeConnError
	outcomeLoginError
)

type outcome struct {
	kind         outcomeKind
	blockSeconds int
	detail       string
	connDur      time.Duration // dial -> connected
	readyDur     time.Duration // connected -> ready
	loginDur     time.Duration // ready -> login returned
}

var digitsRe = regexp.MustCompile(`\d+`)

func parseBlockSeconds(msg string) int {
	if m := digitsRe.FindString(msg); m != "" {
		if n, err := strconv.Atoi(m); err == nil {
			return n
		}
	}
	return 0
}

// probeHandler captures an ip_timed_out error pushed during login.
type probeHandler struct {
	mu           sync.Mutex
	ipTimedOut   bool
	blockSeconds int
	message      string
}

func (h *probeHandler) OnConnected(*game.State) {}
func (h *probeHandler) OnDisconnected(error)    {}

func (h *probeHandler) OnMessage(resp protocol.Response) {
	if resp.Type != protocol.TypeError {
		return
	}
	if code, _ := resp.Payload["code"].(string); code == "ip_timed_out" {
		h.mu.Lock()
		h.ipTimedOut = true
		if m, ok := resp.Payload["message"].(string); ok {
			h.message = m
			h.blockSeconds = parseBlockSeconds(m)
		}
		h.mu.Unlock()
	}
}

func (h *probeHandler) result() (bool, int, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ipTimedOut, h.blockSeconds, h.message
}

func isRateLimitErr(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "ip_timed_out") || strings.Contains(l, "429") ||
		strings.Contains(l, "rate limit") || strings.Contains(l, "too many")
}

// attempt performs one connect + login and reports the outcome. It always
// closes the connection before returning.
func attempt(ctx context.Context, url string, creds credentials, settle time.Duration, debug bool) outcome {
	gameLog := log.New(io.Discard, "", 0)
	if debug {
		gameLog = log.New(os.Stderr, "[game] ", 0)
	}
	client := game.NewClient(url, creds.Username, creds.Password, gameLog)
	client.SetDebugLogging(debug)
	h := &probeHandler{}
	client.SetHandler(h)

	actx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	t0 := time.Now()
	if err := client.Connect(actx); err != nil {
		_ = client.Close()
		if isRateLimitErr(err.Error()) {
			return outcome{kind: outcomeRateLimited, detail: "rate-limited on connect: " + err.Error()}
		}
		return outcome{kind: outcomeConnError, detail: err.Error()}
	}
	// Close blocks up to 5s draining goroutines (client.go); detach it so the
	// probe's measured rate reflects the server-facing connect+login latency,
	// not the client teardown timeout.
	defer func() { go func() { _ = client.Close() }() }()
	connDur := time.Since(t0)

	t1 := time.Now()
	select {
	case <-client.Ready():
	case <-actx.Done():
		return outcome{kind: outcomeConnError, detail: "timed out waiting for ready", connDur: connDur}
	}
	readyDur := time.Since(t1)

	if settle > 0 {
		time.Sleep(settle)
	}

	t2 := time.Now()
	lerr := client.Login(actx)
	loginDur := time.Since(t2)
	o := outcome{connDur: connDur, readyDur: readyDur, loginDur: loginDur}
	if blocked, secs, msg := h.result(); blocked {
		o.kind, o.blockSeconds, o.detail = outcomeRateLimited, secs, msg
		return o
	}
	if lerr != nil {
		if isRateLimitErr(lerr.Error()) {
			o.kind, o.detail = outcomeRateLimited, lerr.Error()
			return o
		}
		o.kind, o.detail = outcomeLoginError, lerr.Error()
		return o
	}
	o.kind = outcomeSuccess
	return o
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

func phases(o outcome) string {
	return fmt.Sprintf("conn=%v ready=%v login=%v",
		o.connDur.Round(time.Millisecond), o.readyDur.Round(time.Millisecond), o.loginDur.Round(time.Millisecond))
}

func runBurst(ctx context.Context, url string, creds credentials, maxAttempts, concurrency int, settle time.Duration, debug bool) {
	if concurrency < 1 {
		concurrency = 1
	}
	fmt.Printf("=== BURST mode: up to %d logins, %d at a time, as %s ===\n", maxAttempts, concurrency, creds.Username)
	start := time.Now()
	successes := 0
	done := 0
	wave := 0
	for done < maxAttempts {
		if ctx.Err() != nil {
			fmt.Println("\ninterrupted.")
			return
		}
		wave++
		n := concurrency
		if rem := maxAttempts - done; rem < n {
			n = rem
		}
		results := make([]outcome, n)
		var wg sync.WaitGroup
		for j := range n {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results[idx] = attempt(ctx, url, creds, settle, debug)
			}(j)
		}
		wg.Wait()
		elapsed := time.Since(start).Round(time.Millisecond)

		var blocked *outcome
		for k := range results {
			o := results[k]
			done++
			switch o.kind {
			case outcomeSuccess:
				successes++
				fmt.Printf("  w%d #%-2d  %-12s  t=%v  %s\n", wave, done, "ok", elapsed, phases(o))
			case outcomeRateLimited:
				fmt.Printf("  w%d #%-2d  %-12s  t=%v  block=%ds  %q\n", wave, done, "RATE-LIMITED", elapsed, o.blockSeconds, o.detail)
				if blocked == nil {
					oc := o
					blocked = &oc
				}
			case outcomeConnError:
				fmt.Printf("  w%d #%-2d  %-12s  t=%v  %s\n", wave, done, "conn-error", elapsed, o.detail)
			case outcomeLoginError:
				fmt.Printf("  w%d #%-2d  %-12s  t=%v  %s\n", wave, done, "login-error", elapsed, o.detail)
			}
		}
		if blocked != nil {
			fmt.Printf("\nRESULT: %d successful logins before the per-IP block tripped (%v, concurrency %d).\n",
				successes, elapsed, concurrency)
			if blocked.blockSeconds > 0 {
				fmt.Printf("        Server block: %ds (+ up to %v client jitter).\n", blocked.blockSeconds, game.SleepIPRateLimitJitter)
			}
			rate := float64(successes) / time.Since(start).Minutes()
			fmt.Printf("        ~%d logins / %v  (~%.0f/min) before block.\n", successes, elapsed, rate)
			fmt.Printf("        Stagger at %v = %.0f/min.\n", game.SleepMedium, 60.0/game.SleepMedium.Seconds())
			return
		}
	}
	fmt.Printf("\nRESULT: reached --max=%d with %d successes and NO block in %v (concurrency %d). Raise --max/--concurrency to find the ceiling.\n",
		maxAttempts, successes, time.Since(start).Round(time.Millisecond), concurrency)
}

func runPaced(ctx context.Context, url string, creds credentials, interval, duration, settle time.Duration, debug bool) {
	fmt.Printf("=== PACED mode: one login every %v for %v as %s ===\n", interval, duration, creds.Username)
	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	successes := 0
	i := 0
	for {
		i++
		o := attempt(ctx, url, creds, settle, debug)
		elapsed := time.Since(start).Round(time.Millisecond)
		switch o.kind {
		case outcomeSuccess:
			successes++
			fmt.Printf("  #%-2d  %-12s  t=%v\n", i, "ok", elapsed)
		case outcomeRateLimited:
			fmt.Printf("  #%-2d  %-12s  t=%v  block=%ds  %q\n", i, "RATE-LIMITED", elapsed, o.blockSeconds, o.detail)
			fmt.Printf("\nRESULT: pace of one login per %v is TOO FAST — blocked after %d successes (%v).\n",
				interval, successes, elapsed)
			return
		case outcomeConnError:
			fmt.Printf("  #%-2d  %-12s  t=%v  %s\n", i, "conn-error", elapsed, o.detail)
		case outcomeLoginError:
			fmt.Printf("  #%-2d  %-12s  t=%v  %s\n", i, "login-error", elapsed, o.detail)
		}
		if time.Now().After(deadline) {
			fmt.Printf("\nRESULT: %d logins at one per %v over %v with NO block — this pace is safe.\n",
				successes, interval, time.Since(start).Round(time.Second))
			return
		}
		select {
		case <-ctx.Done():
			fmt.Println("\ninterrupted.")
			return
		case <-ticker.C:
		}
	}
}

// runClerk performs count paced ws-token logins, minting one token per login.
// It stops at the first error.
func runClerk(ctx context.Context, url string, creds *game.Credentials, count int, interval time.Duration, debug bool) {
	key, err := clerk.LoadKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: no Clerk key: set SPACEMOLT_CLERK_API_KEY or ~/.config/spacemolt/clerk_api_key")
		os.Exit(2)
	}
	src := clerk.NewSource(key)
	fmt.Printf("=== CLERK mode: %d ws-token logins, one per %v, as %s ===\n", count, interval, creds.Username)

	gameLog := log.New(io.Discard, "", 0)
	if debug {
		gameLog = log.New(os.Stderr, "[game] ", 0)
	}

	start := time.Now()
	done, rateLimited := 0, 0
	for i := range count {
		if ctx.Err() != nil {
			fmt.Println("\ninterrupted.")
			break
		}
		t0 := time.Now()
		if err := clerkLoginOnce(ctx, src, url, creds, gameLog, debug); err != nil {
			l := strings.ToLower(err.Error())
			if (strings.Contains(l, "rate") && strings.Contains(l, "limit")) || strings.Contains(l, "429") {
				rateLimited++
			}
			fmt.Printf("  #%-2d  error  %v\n", i+1, err)
			break
		}
		done++
		fmt.Printf("  #%-2d  ok %d\n", i+1, time.Since(t0).Milliseconds())
		if i == count-1 {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
	}
	el := time.Since(start)
	rate := 0.0
	if el > 0 {
		rate = float64(done) / el.Minutes()
	}
	fmt.Printf("\n%d logins in %v (%.1f/min), %d rate_limited\n", done, el.Round(time.Millisecond), rate, rateLimited)
}

// clerkLoginOnce mints a token and logs in over a fresh connection.
func clerkLoginOnce(ctx context.Context, src *clerk.Source, url string, creds *game.Credentials, gameLog *log.Logger, debug bool) error {
	tok, err := src.MintWSToken(ctx, creds.PlayerID)
	if err != nil {
		return fmt.Errorf("mint: %w", err)
	}
	client := game.NewClient(url, creds.Username, creds.Password, gameLog)
	client.SetDebugLogging(debug)
	actx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := client.Connect(actx); err != nil {
		_ = client.Close()
		return fmt.Errorf("connect: %w", err)
	}
	// Close blocks while draining goroutines; detach so timing reflects login.
	defer func() { go func() { _ = client.Close() }() }()
	rctx, rcancel := context.WithTimeout(actx, game.SleepTick)
	defer rcancel()
	select {
	case <-client.Ready():
	case <-rctx.Done():
		return fmt.Errorf("timed out waiting for ready")
	}
	if err := client.LoginWithToken(actx, tok.Value); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	return nil
}

func main() {
	mode := flag.String("mode", "burst", "burst | paced | clerk")
	count := flag.Int("count", 20, "clerk: number of ws-token logins")
	account := flag.String("account", "", "agent id under data/agents/<id>/credentials.json (required)")
	url := flag.String("url", defaultURL, "game server websocket URL")
	maxAttempts := flag.Int("max", 30, "burst: max login attempts before giving up")
	concurrency := flag.Int("concurrency", 1, "burst: simultaneous logins per wave")
	interval := flag.Duration("interval", game.SleepMedium, "paced/clerk: delay between logins")
	duration := flag.Duration("duration", 2*time.Minute, "paced: total run time")
	settle := flag.Duration("settle", 0, "pause between ready and login (0 = none)")
	debug := flag.Bool("debug", false, "verbose game-client logging to stderr")
	flag.Parse()

	if *account == "" {
		fmt.Fprintln(os.Stderr, "error: --account is required (e.g. --account miner-1)")
		flag.Usage()
		os.Exit(2)
	}

	if *mode == "clerk" {
		cc, err := game.LoadCredentials(filepath.Join("data", "agents", *account))
		if err != nil {
			log.Fatalf("credentials: %v", err)
		}
		if cc.PlayerID == "" {
			fmt.Fprintln(os.Stderr, "error: credentials have no player_id: run clerk-sync first")
			os.Exit(2)
		}
		fmt.Printf("Target: %s\n\n", *url)
		runClerk(context.Background(), *url, cc, *count, *interval, *debug)
		return
	}

	creds, err := loadCreds(*account)
	if err != nil {
		log.Fatalf("credentials: %v", err)
	}

	fmt.Printf("Target: %s   (tripping the limit locks out THIS IP for the reported block + jitter)\n\n", *url)
	ctx := context.Background()

	switch *mode {
	case "burst":
		runBurst(ctx, *url, creds, *maxAttempts, *concurrency, *settle, *debug)
	case "paced":
		runPaced(ctx, *url, creds, *interval, *duration, *settle, *debug)
	default:
		log.Fatalf("unknown --mode %q (want burst|paced|clerk)", *mode)
	}
}
