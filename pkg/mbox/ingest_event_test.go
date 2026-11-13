package mbox

import (
	"strings"
	"testing"
)

// HandleEvent stores a non-chat push in the "event" channel with its type as
// the sender, so sweep ships keep every broadcast from uncharted systems.
func TestHandleEventStoresPush(t *testing.T) {
	s := newTestStore(t)
	ing := NewIngester(s)
	ing.HandleEvent("signal_fragment", map[string]any{"message": "QX7 ...", "system_id": "bare_lamp"})
	ing.HandleEvent("signal_fragment", map[string]any{"message": "QX8 ...", "system_id": "bare_lamp"})
	msgs, err := s.List(Query{Channel: "event", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("stored %d events, want 2", len(msgs))
	}
	m := msgs[0]
	if m.Sender != "signal_fragment" || m.Source != "push" || !strings.Contains(m.Content, "bare_lamp") {
		t.Errorf("event = %+v", m)
	}
}
