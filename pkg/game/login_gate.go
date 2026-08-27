package game

import "context"

// dialWithGate performs one gated authentication attempt: it waits for this
// process's turn at the host-wide gate, then connects and authenticates.
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
func dialWithGate(ctx context.Context, g *ReconnectGate, connect, login func(context.Context) error) error {
	if err := g.Acquire(ctx); err != nil {
		return err
	}
	if err := connect(ctx); err != nil {
		recordRateLimitBlock(g, err)
		return err
	}
	if err := login(ctx); err != nil {
		recordRateLimitBlock(g, err)
		return err
	}
	return nil
}

// recordRateLimitBlock publishes a per-IP block discovered in err to the
// host-wide gate so every other client honors it and it expires instead of
// being re-triggered. Errors that are not rate limits (a refused dial, bad
// credentials) are ignored — stalling the whole host on those would be worse
// than the failure itself.
func recordRateLimitBlock(g *ReconnectGate, err error) {
	if g == nil || err == nil {
		return
	}
	if d, ok := rateLimitBlock(err.Error(), reconnectBlockDefault); ok {
		_ = g.RecordBlock(d)
	}
}
