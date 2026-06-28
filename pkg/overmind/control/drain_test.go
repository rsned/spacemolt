package control

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDrainEnvelopeRoundTrip(t *testing.T) {
	env, err := NewEnvelope(TypeDrain, "trader-1", nil)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(env); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := NewDecoder(&buf).Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != TypeDrain {
		t.Fatalf("Type = %q, want %q", got.Type, TypeDrain)
	}
}

func TestStatusDrainedJSONRoundTrip(t *testing.T) {
	raw, err := json.Marshal(Status{Drained: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var st Status
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !st.Drained {
		t.Fatalf("Drained round-trip lost: %s", raw)
	}
}
