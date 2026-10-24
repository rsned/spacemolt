package game

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game/clerk"
)

// A fresh worker start dialed and authenticated outside the host-wide gate:
// InitializeAgent attached the gate to the ReconnectingHandler (reconnects
// only) and then called Connect/Login ungated. Live 2026-08-26: restarting 16
// miners at ~12 logins/min tripped the per-IP auth budget and produced 429s
// and login timeouts across the mining, haul and unlock fleets.
func TestDialWithGate_AcquiresBeforeConnecting(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)

	var order []string
	a := authAttempt{
		connect: func(context.Context) error { order = append(order, "connect"); return nil },
		login:   func(context.Context, string) error { order = append(order, "login"); return nil },
	}

	// First dial claims the free slot without waiting.
	if err := dialWithGate(context.Background(), g, 1, a); err != nil {
		t.Fatalf("first dial: %v", err)
	}
	if len(clk.slept) != 0 {
		t.Fatalf("first dial should not wait, slept=%v", clk.slept)
	}

	// The second dial must wait out the spacing -- that is the whole point:
	// two fresh logins on one host are spaced, not simultaneous.
	if err := dialWithGate(context.Background(), g, 1, a); err != nil {
		t.Fatalf("second dial: %v", err)
	}
	if len(clk.slept) == 0 {
		t.Error("second dial did not wait for the gate; fresh logins are ungated")
	}
	want := []string{"connect", "login", "connect", "login"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("call order = %v, want %v", order, want)
	}
}

// A 429 discovered by one starting worker must reach the whole host, or the
// next N workers each rediscover it and deepen the block.
func TestDialWithGate_PublishesBlockFoundAtLogin(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)

	a := authAttempt{
		connect: func(context.Context) error { return nil },
		login: func(context.Context, string) error {
			return errors.New("login failed: 429 too many requests, try again in 90 seconds")
		},
	}
	if err := dialWithGate(context.Background(), g, 1, a); err == nil {
		t.Fatal("dialWithGate should surface the login error")
	}

	// A second client on the same host must now honor the 90s block, which is
	// far longer than the spacing it would otherwise wait.
	clk2 := &fakeClock{t: clk.t}
	g2 := newTestGate(t, path, testGateCfg, clk2)
	if err := g2.Acquire(context.Background(), DialToken); err != nil {
		t.Fatalf("second client Acquire: %v", err)
	}
	if total := totalSlept(clk2); total < 60*time.Second {
		t.Errorf("second client waited %v; want >= 60s from the published block", total)
	}
}

// A connect failure must not be followed by a login attempt.
func TestDialWithGate_StopsWhenConnectFails(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)

	loginCalls := 0
	a := authAttempt{
		connect: func(context.Context) error { return errors.New("dial tcp: connection refused") },
		login:   func(context.Context, string) error { loginCalls++; return nil },
	}
	if err := dialWithGate(context.Background(), g, 1, a); err == nil {
		t.Fatal("expected the connect error to surface")
	}
	if loginCalls != 0 {
		t.Errorf("login called %d times after a failed connect; want 0", loginCalls)
	}
}

// Direct/test construction leaves the gate nil; that must stay a no-op rather
// than panicking or blocking.
func TestDialWithGate_NilGateStillDials(t *testing.T) {
	connected, loggedIn := false, false
	err := dialWithGate(context.Background(), nil, 1, authAttempt{
		connect: func(context.Context) error { connected = true; return nil },
		login:   func(context.Context, string) error { loggedIn = true; return nil },
	})
	if err != nil {
		t.Fatalf("nil gate: %v", err)
	}
	if !connected || !loggedIn {
		t.Errorf("nil gate skipped work: connected=%v loggedIn=%v", connected, loggedIn)
	}
}

// A cancelled context must abort before dialing, not after, and must not burn
// the remaining tries.
func TestDialWithGate_HonorsCancelledContext(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)
	// Claim the slot so the next Acquire has to wait.
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatalf("priming Acquire: %v", err)
	}
	g.sleep = func(ctx context.Context, _ time.Duration) error { return context.Canceled }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	connectCalls := 0
	err := dialWithGate(ctx, g, dialAttempts, authAttempt{
		connect: func(context.Context) error { connectCalls++; return nil },
		login:   func(context.Context, string) error { return nil },
	})
	if err == nil {
		t.Fatal("expected a context error")
	}
	if connectCalls != 0 {
		t.Errorf("connect called %d times despite a cancelled context; want 0", connectCalls)
	}
}

// recordRateLimitBlock must ignore ordinary errors: a refused dial or a bad
// password is not a per-IP block and must not stall the whole host.
func TestRecordRateLimitBlock_IgnoresNonRateLimitErrors(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)

	recordRateLimitBlock(g, errors.New("login failed: invalid credentials"), "test")

	clk2 := &fakeClock{t: clk.t}
	g2 := newTestGate(t, path, testGateCfg, clk2)
	if err := g2.Acquire(context.Background(), DialToken); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if total := totalSlept(clk2); total >= 60*time.Second {
		t.Errorf("a bad-credentials error published a %v block; want none", total)
	}
}

// The ws-token is single-use and expires in ~5 minutes, so minting before the
// gate grants the slot can hand login a token that died in the queue. The
// order per try is fixed: acquire, mint, connect, login.
func TestDialWithGate_MintsAfterAcquire(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)
	// Prime the gate so the next Acquire must sleep; the sleep marks "acquire".
	if err := g.Acquire(context.Background(), DialToken); err != nil {
		t.Fatalf("priming Acquire: %v", err)
	}
	var order []string
	g.sleep = func(ctx context.Context, d time.Duration) error {
		order = append(order, "acquire")
		return clk.sleep(ctx, d)
	}

	var gotToken string
	a := authAttempt{
		mint:    func(context.Context) (string, error) { order = append(order, "mint"); return "tok-1", nil },
		connect: func(context.Context) error { order = append(order, "connect"); return nil },
		login: func(_ context.Context, tok string) error {
			order = append(order, "login")
			gotToken = tok
			return nil
		},
	}
	if err := dialWithGate(context.Background(), g, dialAttempts, a); err != nil {
		t.Fatalf("dialWithGate: %v", err)
	}
	want := []string{"acquire", "mint", "connect", "login"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("call order = %v, want %v", order, want)
	}
	if gotToken != "tok-1" {
		t.Errorf("login token = %q, want the minted %q", gotToken, "tok-1")
	}
	if st := readGateFile(t, path); len(st.PasswordLogins) != 0 {
		t.Errorf("token dial consumed the password budget: %v", st.PasswordLogins)
	}
}

// A mint 429 is the Clerk endpoint metering this host. It must become a
// fleet-wide block (so siblings do not hammer the mint too) and must NOT fall
// back to a password login, which would spend the scarcer budget.
func TestDialWithGate_Mint429RecordsBlockAndRetries(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)
	start := clk.t

	mints := 0
	var logins []string
	a := authAttempt{
		mint: func(context.Context) (string, error) {
			mints++
			if mints == 1 {
				return "", &clerk.HTTPError{Status: 429, Method: "POST", URL: "/api/player/p/ws-token", RetryAfter: 7 * time.Second}
			}
			return "tok-2", nil
		},
		connect: func(context.Context) error { return nil },
		login:   func(_ context.Context, tok string) error { logins = append(logins, tok); return nil },
	}
	if err := dialWithGate(context.Background(), g, dialAttempts, a); err != nil {
		t.Fatalf("dialWithGate: %v", err)
	}
	st := readGateFile(t, path)
	if st.BlockSource != "mint429" {
		t.Errorf("block_source = %q, want mint429", st.BlockSource)
	}
	if want := start.Add(7 * time.Second).UnixNano(); st.BlockedUntilNanos != want {
		t.Errorf("blocked_until = %v, want %v", time.Unix(0, st.BlockedUntilNanos), time.Unix(0, want))
	}
	if len(logins) != 1 || logins[0] != "tok-2" {
		t.Errorf("logins = %q, want exactly one with tok-2 (no password fallback)", logins)
	}
	if got := totalSlept(clk); got < 7*time.Second {
		t.Errorf("second try waited %v; want >= 7s for the mint block", got)
	}
	if len(st.PasswordLogins) != 0 {
		t.Errorf("mint 429 fell back to password: password_logins=%v", st.PasswordLogins)
	}
}

// A non-rate-limit mint failure (revoked key, unowned player) falls back to a
// password login, which must be charged to the password budget. The warning is
// logged once per process, not once per try, or a bad key floods every log.
func TestDialWithGate_MintAuthFailureFallsBackToPasswordBudget(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)

	var buf bytes.Buffer
	var logins []string
	a := authAttempt{
		mint: func(context.Context) (string, error) {
			return "", &clerk.HTTPError{Status: 401, Method: "POST", URL: "/api/player/p/ws-token"}
		},
		connect:      func(context.Context) error { return nil },
		login:        func(_ context.Context, tok string) error { logins = append(logins, tok); return nil },
		logger:       log.New(&buf, "", 0),
		fallbackOnce: new(sync.Once),
	}
	if err := dialWithGate(context.Background(), g, dialAttempts, a); err != nil {
		t.Fatalf("dialWithGate: %v", err)
	}
	if len(logins) != 1 || logins[0] != "" {
		t.Fatalf("logins = %q, want one password login (empty token)", logins)
	}
	if st := readGateFile(t, path); len(st.PasswordLogins) != 1 {
		t.Errorf("password_logins = %v, want exactly one entry", st.PasswordLogins)
	}

	// Three more failing tries sharing the process-wide once: still one line.
	a.connect = func(context.Context) error { return errors.New("dial tcp: connection refused") }
	if err := dialWithGate(context.Background(), g, dialAttempts, a); err == nil {
		t.Fatal("expected the connect error to surface")
	}
	if n := strings.Count(buf.String(), "falling back to password"); n != 1 {
		t.Errorf("fallback log appeared %d times across 4 tries, want 1:\n%s", n, buf.String())
	}
}

// dialWithGate is the only retry mechanism for a fresh start: exactly
// `attempts` gated tries, then the last error.
func TestDialWithGate_StopsAfterAttempts(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	g := newTestGate(t, filepath.Join(t.TempDir(), "g"), testGateCfg, clk)

	connects := 0
	a := authAttempt{
		connect: func(context.Context) error { connects++; return errors.New("dial tcp: connection refused") },
		login:   func(context.Context, string) error { t.Error("login after failed connect"); return nil },
	}
	err := dialWithGate(context.Background(), g, dialAttempts, a)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v, want the last connect error", err)
	}
	if connects != 3 {
		t.Errorf("connect called %d times, want 3", connects)
	}
}

// Without a minter every dial is a password login and is charged as one.
func TestDialWithGate_NoMinterIsPasswordKind(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)

	a := authAttempt{
		connect: func(context.Context) error { return nil },
		login:   func(context.Context, string) error { return nil },
	}
	if err := dialWithGate(context.Background(), g, dialAttempts, a); err != nil {
		t.Fatalf("dialWithGate: %v", err)
	}
	st := readGateFile(t, path)
	if len(st.PasswordLogins) != 1 || len(st.Dials) != 1 {
		t.Errorf("dials=%v password_logins=%v, want one of each", st.Dials, st.PasswordLogins)
	}
}

// A caller re-recording a mint 429 (the ReconnectingHandler does, after a
// failed Reconnect) must keep the server's 7s, not stretch it to the 5-minute
// bare-429 default the error text alone would yield.
func TestRecordRateLimitBlock_ClerkRetryAfterNotInflated(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)

	err := fmt.Errorf("reconnect: %w", &clerk.HTTPError{Status: 429, Method: "POST", URL: "/x", RetryAfter: 7 * time.Second})
	recordRateLimitBlock(g, err, "reconnect")
	if got, want := readGateFile(t, path).BlockedUntilNanos, clk.t.Add(7*time.Second).UnixNano(); got != want {
		t.Errorf("blocked for %v, want 7s", time.Duration(got-clk.t.UnixNano()))
	}

	// A non-429 clerk error is not a per-IP block.
	path2 := filepath.Join(t.TempDir(), "g2")
	g2 := newTestGate(t, path2, testGateCfg, clk)
	recordRateLimitBlock(g2, &clerk.HTTPError{Status: 401, Method: "POST", URL: "/x"}, "reconnect")
	if err := g2.Acquire(context.Background(), DialToken); err != nil {
		t.Fatal(err)
	}
	if st := readGateFile(t, path2); st.BlockedUntilNanos != 0 {
		t.Errorf("a 401 mint error recorded a block: %+v", st)
	}
}

// The per-player login_token limit is the account's, not the host's: spec §4
// says it waits retry_after locally and re-mints, never blocking the fleet.
func TestServerError_BucketPopulated(t *testing.T) {
	err := serverErrorFromPayload(map[string]any{
		"code":    "rate_limited",
		"message": "too many login_token calls",
		"details": map[string]any{"limit": "clerk_login_token", "retry_after": float64(3)},
	})
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("not a *ServerError: %T", err)
	}
	if se.Bucket != "clerk_login_token" || se.RetryAfter != 3*time.Second {
		t.Errorf("bucket=%q retry_after=%v, want clerk_login_token/3s", se.Bucket, se.RetryAfter)
	}
}

func TestRecordRateLimitBlock_SkipsPerPlayerBucket(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)

	err := fmt.Errorf("login: %w", &ServerError{Code: "rate_limited", Bucket: perPlayerLoginBucket, RetryAfter: 30 * time.Second})
	recordRateLimitBlock(g, err, "login")
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("gate file touched by a per-player limit (stat err=%v)", statErr)
	}
}

func TestDialWithGate_PerPlayerRateLimitWaitsLocally(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)

	mints, logins := 0, 0
	var sleptBeforeSecond []time.Duration
	a := authAttempt{
		mint:    func(context.Context) (string, error) { mints++; return fmt.Sprintf("tok-%d", mints), nil },
		connect: func(context.Context) error { return nil },
		login: func(context.Context, string) error {
			logins++
			if logins == 1 {
				return &ServerError{Code: "rate_limited", Bucket: perPlayerLoginBucket, RetryAfter: 3 * time.Second}
			}
			sleptBeforeSecond = slices.Clone(clk.slept)
			return nil
		},
	}
	if err := dialWithGate(context.Background(), g, 2, a); err != nil {
		t.Fatalf("dialWithGate: %v", err)
	}
	if mints != 2 || logins != 2 {
		t.Errorf("mints=%d logins=%d, want 2/2 (re-mint after the local wait)", mints, logins)
	}
	if !slices.Contains(sleptBeforeSecond, 3*time.Second) {
		t.Errorf("slept %v before the second try; want a 3s local wait", sleptBeforeSecond)
	}
	if st := readGateFile(t, path); st.BlockedUntilNanos != 0 {
		t.Errorf("per-player limit recorded a fleet-wide block: %+v", st)
	}
}

// A 503 from the mint endpoint means Clerk is down, not that the key is bad:
// the operator's guidance is to wait Retry-After and retry, never to fall back
// (or rotate). The wait is fleet-wide — one key, one Clerk — like a mint 429.
func TestDialWithGate_Mint503WaitsAndRetries(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)
	start := clk.t

	mints := 0
	var logins []string
	a := authAttempt{
		mint: func(context.Context) (string, error) {
			mints++
			if mints == 1 {
				return "", &clerk.HTTPError{Status: 503, Method: "POST", URL: "/api/player/p/ws-token", RetryAfter: 4 * time.Second}
			}
			return "tok-2", nil
		},
		connect: func(context.Context) error { return nil },
		login:   func(_ context.Context, tok string) error { logins = append(logins, tok); return nil },
	}
	if err := dialWithGate(context.Background(), g, dialAttempts, a); err != nil {
		t.Fatalf("dialWithGate: %v", err)
	}
	st := readGateFile(t, path)
	if st.BlockSource != "mint503" {
		t.Errorf("block_source = %q, want mint503", st.BlockSource)
	}
	if want := start.Add(4 * time.Second).UnixNano(); st.BlockedUntilNanos != want {
		t.Errorf("blocked_until = %v, want %v", time.Unix(0, st.BlockedUntilNanos), time.Unix(0, want))
	}
	if len(logins) != 1 || logins[0] != "tok-2" {
		t.Errorf("logins = %q, want exactly one with tok-2 (no password fallback)", logins)
	}
	if len(st.PasswordLogins) != 0 {
		t.Errorf("mint 503 fell back to password: password_logins=%v", st.PasswordLogins)
	}
}

// A 503 with no Retry-After is an outage of unknown length, not a rate limit:
// wait one window, not the 5-minute bare-block default.
func TestDialWithGate_Mint503NoRetryAfterWaitsOneWindow(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)
	start := clk.t

	a := authAttempt{
		mint: func(context.Context) (string, error) {
			return "", &clerk.HTTPError{Status: 503, Method: "POST", URL: "/x"}
		},
		connect: func(context.Context) error { t.Fatal("connect must not run after a mint 503"); return nil },
		login:   func(context.Context, string) error { return nil },
	}
	if err := dialWithGate(context.Background(), g, 1, a); err == nil {
		t.Fatal("expected the mint error back")
	}
	st := readGateFile(t, path)
	if want := start.Add(ConnectWindow).UnixNano(); st.BlockedUntilNanos != want {
		t.Errorf("blocked_until = %v, want start+ConnectWindow %v", time.Unix(0, st.BlockedUntilNanos), time.Unix(0, want))
	}
}

// A DNS failure is the host's resolver, not the server's rate limiter. On
// 2026-10-22 one parked the whole fleet for five minutes because the error text
// happened to contain "429" inside an ephemeral port number.
func TestRecordRateLimitBlock_IgnoresDNSError(t *testing.T) {
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	path := filepath.Join(t.TempDir(), "g")
	g := newTestGate(t, path, testGateCfg, clk)

	dnsErr := &net.DNSError{Err: "read udp 127.0.0.1:42901->127.0.0.53:53: i/o timeout", Name: "game.spacemolt.com", IsTimeout: true}
	wrapped := fmt.Errorf("failed to connect: failed to WebSocket dial: %w", &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr})
	recordRateLimitBlock(g, wrapped, "connect")

	// Nothing may have been recorded: the gate file does not even exist until
	// the first write, so a missing file is the expected outcome.
	if _, err := os.Stat(path); err == nil {
		st := readGateFile(t, path)
		if st.BlockedUntilNanos != 0 {
			t.Fatalf("DNS error recorded a block until %v (source %q); want none", time.Unix(0, st.BlockedUntilNanos), st.BlockSource)
		}
	}
}
