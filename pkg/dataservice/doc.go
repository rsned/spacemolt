// Package dataservice provides an agent-to-agent query service. A host
// agent runs the service, which listens for private chat DMs, parses
// them as data queries via a pluggable Handler registry, executes
// them against the shared knowledge base, and replies in the same
// format (plaintext or JSON) as the request.
package dataservice
