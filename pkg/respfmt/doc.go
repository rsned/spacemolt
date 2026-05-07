// Package respfmt produces human-readable, styled renderings of game
// server responses. It exists so multiple CLIs (play_as, bulk-buy-order,
// future tools) can present command output the same way without each
// re-implementing the same struct unmarshal + table layout logic.
//
// # Public surface
//
//   - Format(command, raw, opts...) — dispatch by command name, return the
//     styled rendering or "" if no formatter is registered.
//   - Error(err, command) — friendly one-line error rendering, including
//     command-specific overrides (e.g. mine "depleted" → "Ore depleted.").
//   - Per-command exports (Storage, Market, …) for callers that already
//     know exactly which renderer they want.
//
// # Scope
//
// respfmt only produces styled output. Raw/JSON pass-through belongs in
// the caller. respfmt does not perform any I/O, network calls, or read
// from process state — every input is supplied via the function arguments.
//
// # Migration
//
// Formatters are being moved from cmd/tools/play_as/main.go in stages.
// The package is intentionally small at first; expect the surface to grow
// as each migration lands.
package respfmt
