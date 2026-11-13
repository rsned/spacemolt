package main

import "fmt"

// passthroughPositional names the positional parameters of commands that
// reach the generic passthrough but reject the arg1/arg2 fallback. The server
// validates parameter names strictly: `faction_scan_poi the_weld` sent arg1
// and got "Unknown parameter(s): arg1. Valid parameters: poi_id." (2026-11-12).
// Add a command here when the server names its parameters; names come from
// server_docs/api.md signatures.
var passthroughPositional = map[string][]string{
	"faction_scan_poi": {"poi_id"},
}

// passthroughParamName is the payload key for the n-th (1-based) bare
// positional argument of cmd: the documented name when known, else "argN".
func passthroughParamName(cmd string, n int) string {
	if names := passthroughPositional[cmd]; n >= 1 && n <= len(names) {
		return names[n-1]
	}
	return fmt.Sprintf("arg%d", n)
}
