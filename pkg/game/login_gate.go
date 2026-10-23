package game

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/rsned/spacemolt/pkg/game/clerk"
)

// dialAttempts is how many gated tries a fresh start makes before giving up.
// The gate is the only retry mechanism: each try claims its own slot, so the
// spacing between tries is the gate's, never a local sleep.
const dialAttempts = 3

// TokenMinter mints one single-use Clerk ws-token for this client's player.
type TokenMinter func(ctx context.Context) (string, error)

// authAttempt is one way to connect and authenticate. Both the fresh-start
// path (dialWithGate) and Client.Reconnect drive it through try, so the
// mint/fallback rules live in exactly one place.
type authAttempt struct {
	mint    TokenMinter                                   // nil → password only
	connect func(ctx context.Context) error               // dial + wait for ready
	login   func(ctx context.Context, token string) error // token == "" → password

	// logger receives the mint-fallback warning; nil means log.Default().
	logger *log.Logger
	// fallbackOnce limits the mint-fallback warning to once per process. A
	// Client passes its own so the initial dial and every reconnect share it;
	// nil means once per dialWithGate call.
	fallbackOnce *sync.Once
}

// try performs one gated authentication: Acquire → mint → connect → login.
//
// The token is single-use and lives ~5 minutes, so it is minted only after the
// gate grants the slot; minting first could hand login a token that expired in
// the queue. A rate-limited mint is a fleet-wide block and does not fall back
// (the password budget is scarcer than the dial budget). Any other mint error
// falls back to the password login and is charged to the password budget.
func (a *authAttempt) try(ctx context.Context, g *ReconnectGate) error {
	kind := DialPassword
	if a.mint != nil {
		kind = DialToken
	}
	if err := g.Acquire(ctx, kind); err != nil {
		return err
	}

	token := ""
	if a.mint != nil {
		tok, err := a.mint(ctx)
		var he *clerk.HTTPError
		switch {
		case err == nil:
			token = tok
		case errors.As(err, &he) && he.RateLimited():
			d := he.RetryAfter
			if d <= 0 {
				d = g.DefaultBlock()
			}
			_ = g.RecordBlock(d, "mint429")
			return err
		default:
			a.logFallback(err)
			if err := g.Acquire(ctx, DialPassword); err != nil {
				return err
			}
		}
	}

	if err := a.connect(ctx); err != nil {
		recordRateLimitBlock(g, err, "connect")
		return err
	}
	if err := a.login(ctx, token); err != nil {
		recordRateLimitBlock(g, err, "login")
		return err
	}
	return nil
}

// logFallback emits the mint-fallback warning at most once per fallbackOnce.
// The error is a *clerk.HTTPError or transport error, neither of which carries
// the API key or a token.
func (a *authAttempt) logFallback(err error) {
	logger := a.logger
	if logger == nil {
		logger = log.Default()
	}
	logIt := func() { logger.Printf("clerk mint failed (%v); falling back to password", err) }
	if a.fallbackOnce == nil {
		logIt()
		return
	}
	a.fallbackOnce.Do(logIt)
}

// isAuthFrame reports whether an outbound frame type carries a credential
// (password or ws-token), whose payload must never be logged.
func isAuthFrame(msgType string) bool {
	return msgType == "login" || msgType == "login_token"
}

// dialWithGate makes up to attempts gated authentication tries (attempts <= 0
// means one) and returns nil on the first success, else the last error.
//
// The gate exists because the server budgets authentication per IP, not per
// account, so every client on one host draws on a single shared allowance.
// Reconnects have been coordinated since the gate was introduced, but a fresh
// start dialed and authenticated outside it: InitializeAgent attached the gate
// to the ReconnectingHandler and then called Connect/Login directly. Live
// 2026-08-26, restarting 16 miners spawned 16 ungated logins and produced 429s
// plus login timeouts across three fleets.
//
// A nil gate is a no-op, which keeps direct and test construction uncoordinated
// by default.
func dialWithGate(ctx context.Context, g *ReconnectGate, attempts int, a authAttempt) error {
	attempts = max(attempts, 1)
	if a.fallbackOnce == nil {
		a.fallbackOnce = new(sync.Once)
	}
	var err error
	for range attempts {
		if err = a.try(ctx, g); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
	}
	return err
}

// recordRateLimitBlock publishes a per-IP block discovered in err to the
// host-wide gate so every other client honors it and it expires instead of
// being re-triggered. Errors that are not rate limits (a refused dial, bad
// credentials) are ignored — stalling the whole host on those would be worse
// than the failure itself.
//
// Structured waits are read before the text: a *ServerError's retry_after and
// a rate-limited *clerk.HTTPError's Retry-After. The clerk error's text names
// "429" but not in a form rateLimitBlock parses, so without this a caller
// re-recording a 7s mint block would stretch it to the 5-minute default.
func recordRateLimitBlock(g *ReconnectGate, err error, source string) {
	if g == nil || err == nil {
		return
	}
	var he *clerk.HTTPError
	if errors.As(err, &he) {
		if !he.RateLimited() {
			return
		}
		d := he.RetryAfter
		if d <= 0 {
			d = g.DefaultBlock()
		}
		_ = g.RecordBlock(d, source)
		return
	}
	var se *ServerError
	if errors.As(err, &se) && se.RetryAfter > 0 {
		_ = g.RecordBlock(se.RetryAfter, source)
		return
	}
	if d, ok := rateLimitBlock(err.Error(), g.DefaultBlock()); ok {
		_ = g.RecordBlock(d, source)
	}
}
