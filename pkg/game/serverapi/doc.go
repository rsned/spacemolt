// Package serverapi defines types that mirror the game server's JSON wire format.
//
// These types are used exclusively for deserializing server responses and should
// change freely as the server evolves. Internal code should use the stable types
// defined in pkg/game/types.go instead.
//
// Import direction: serverapi imports game (one-way). The game package imports
// serverapi only in client.go for parsing server responses.
//
// Converter functions (e.g. PlayerToInternal) map external types to internal ones,
// filtering superfluous fields and normalizing data.
package serverapi
