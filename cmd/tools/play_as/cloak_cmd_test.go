package main

import (
	"strings"
	"testing"
)

// Every spelling an operator might type must reach the server as the value
// they meant; an unreadable one is an error, never a silent "off".
func TestCloakEnable(t *testing.T) {
	on := []string{"cloak on", "cloak true", "cloak 1", "cloak enable", "cloak --enable true", "cloak --enable=true",
		"cloak enable=true", "cloak enable true", "cloak --enable", "cloak --enable 1", "cloak ON"}
	off := []string{"cloak off", "cloak false", "cloak 0", "cloak disable", "cloak --enable false", "cloak --enable=false",
		"cloak enable=false", "cloak enable false", "cloak --enable 0"}
	for _, line := range on {
		if got, err := cloakEnable(strings.Fields(line)); err != nil || !got {
			t.Errorf("%q = %v, %v; want on", line, got, err)
		}
	}
	for _, line := range off {
		if got, err := cloakEnable(strings.Fields(line)); err != nil || got {
			t.Errorf("%q = %v, %v; want off", line, got, err)
		}
	}
	for _, line := range []string{"cloak", "cloak maybe", "cloak --enable perhaps", "cloak --strength 4"} {
		if _, err := cloakEnable(strings.Fields(line)); err == nil {
			t.Errorf("%q: want a usage error", line)
		}
	}
}
