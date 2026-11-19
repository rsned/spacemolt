package main

import (
	"fmt"
	"strings"
)

const cloakUsage = "usage: cloak on|off (also true/false, 1/0, --enable true|false)"

// cloakEnable reads the on/off argument of a cloak command line. It accepts
// `on`, `true`, `1`, `enable` and their opposites, bare or as the value of
// `enable` / `--enable` (space or = separated); `--enable` alone is on.
// Anything else is an error: a typo must not send "off".
func cloakEnable(parts []string) (bool, error) {
	args := make([]string, 0, 2)
	for _, p := range parts[min(1, len(parts)):] {
		args = append(args, strings.Split(strings.ToLower(p), "=")...)
	}
	keyed := false
	if len(args) > 0 && (args[0] == "--enable" || (args[0] == "enable" && len(args) > 1)) {
		keyed, args = true, args[1:]
	}
	switch {
	case keyed && len(args) == 0:
		return true, nil
	case len(args) != 1:
		return false, fmt.Errorf("%s", cloakUsage)
	}
	switch args[0] {
	case "on", "true", "1", "enable", "yes":
		return true, nil
	case "off", "false", "0", "disable", "no":
		return false, nil
	}
	return false, fmt.Errorf("cloak: %q is not on or off; %s", args[0], cloakUsage)
}
