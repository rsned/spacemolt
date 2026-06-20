package control

import "testing"

func TestEnvelopeRoundTrip(t *testing.T) {
	want := Hello{AgentID: "resident-1", Role: "resident", Station: "ST-9", PID: 42}
	env, err := NewEnvelope(TypeHello, want.AgentID, want)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if env.Type != TypeHello || env.AgentID != "resident-1" {
		t.Fatalf("envelope header wrong: %+v", env)
	}
	var got Hello
	if err := env.Into(&got); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestIntoWrongShapeStillDecodesKnownFields(t *testing.T) {
	env, _ := NewEnvelope(TypeStatus, "a", Status{System: "SOL", Credits: 100})
	var got Status
	if err := env.Into(&got); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if got.System != "SOL" || got.Credits != 100 {
		t.Fatalf("payload lost: %+v", got)
	}
}
