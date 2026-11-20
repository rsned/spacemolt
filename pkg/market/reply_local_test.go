package market

import "testing"

// v0.728.0: view_market takes a base_id and reads any charted station's book
// while docked anywhere. Capture files a book under the docked station, so a
// remote book must not be captured as the local one.
func TestReplyIsLocal(t *testing.T) {
	const cj = "c976fa7df53c87f9616165d7d411b41a"
	for _, tc := range []struct {
		name   string
		raw    string
		docked string
		want   bool
	}{
		{"local read names the docked base", `{"action":"view_market","base_id":"` + cj + `","items":[]}`, cj, true},
		{"remote read names another base", `{"action":"view_market","base_id":"grand_exchange_station","items":[]}`, cj, false},
		{"reply without base_id (pre-0.728) is local", `{"action":"view_market","items":[]}`, cj, true},
		{"docked base unknown: cannot tell, keep capturing", `{"base_id":"grand_exchange_station","items":[]}`, "", true},
		{"unreadable reply is left to the parser", `not json`, cj, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReplyIsLocal([]byte(tc.raw), tc.docked); got != tc.want {
				t.Errorf("ReplyIsLocal = %v, want %v", got, tc.want)
			}
		})
	}
}
