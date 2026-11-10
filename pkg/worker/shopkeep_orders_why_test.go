package worker

import (
	"errors"
	"strings"
	"testing"
)

// The order-book skip line must say WHY: on 2026-11-10 every mb shopkeep pass
// logged a bare "no order book reading; skipping" 150 times in two hours and
// nothing showed which of five failure paths it was.
func TestOrdersReadingWhy(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		raw  string
		want string
	}{
		{"view error", errors.New("timeout waiting for view_orders"), "", "view_orders: timeout waiting for view_orders"},
		{"no cache", nil, "", "no cached view_orders response"},
		{"bad json", nil, `{"orders":`, "unparsable view_orders response"},
		{"has more", nil, `{"action":"view_orders","orders":[],"has_more":true,"page":1}`, "paged (has_more)"},
		{"truncated", nil, `{"action":"view_orders","orders":[],"orders_truncated":true}`, "orders_truncated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, why, ok := parseOwnAsks(tc.err, []byte(tc.raw), "me")
			if ok {
				t.Fatal("want not ok")
			}
			if !strings.Contains(why, tc.want) {
				t.Fatalf("why %q, want it to contain %q", why, tc.want)
			}
		})
	}
	asks, why, ok := parseOwnAsks(nil, []byte(`{"action":"view_orders","orders":[]}`), "me")
	if !ok || why != "" || len(asks) != 0 {
		t.Fatalf("empty book should be a good reading: ok=%v why=%q asks=%v", ok, why, asks)
	}
}
