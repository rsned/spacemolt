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
	env, _ := NewEnvelope(TypeStatus, "a", Status{System: "SOL", Credits: 100, CargoCapacity: 80, CargoUsed: 12})
	var got Status
	if err := env.Into(&got); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if got.System != "SOL" || got.Credits != 100 || got.CargoCapacity != 80 || got.CargoUsed != 12 {
		t.Fatalf("payload lost: %+v", got)
	}
}

func TestAssignRoundTrip(t *testing.T) {
	in := Assign{
		TaskID: "mine-bunda-iron",
		Script: "mining_run",
		Params: map[string]string{"TARGET_SYSTEM": "bunda", "COUNT": "20"},
	}
	env, err := NewEnvelope(TypeAssign, "miner-1", in)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if env.Type != TypeAssign {
		t.Fatalf("type = %q, want %q", env.Type, TypeAssign)
	}
	var out Assign
	if err := env.Into(&out); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if out.TaskID != in.TaskID || out.Script != in.Script || out.Params["TARGET_SYSTEM"] != "bunda" || out.Params["COUNT"] != "20" {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestAdminEnvelopeRoundTrip(t *testing.T) {
	env, err := NewEnvelope(TypeAdminRemove, "craftsman-1", AdminRequest{AgentID: "craftsman-1"})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	var req AdminRequest
	if err := env.Into(&req); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if req.AgentID != "craftsman-1" {
		t.Fatalf("AgentID = %q, want craftsman-1", req.AgentID)
	}
	ack, err := NewEnvelope(TypeAdminAck, "craftsman-1", AdminAck{AgentID: "craftsman-1", Status: AckAccepted})
	if err != nil {
		t.Fatalf("NewEnvelope ack: %v", err)
	}
	var a AdminAck
	if err := ack.Into(&a); err != nil {
		t.Fatalf("Into ack: %v", err)
	}
	if a.Status != AckAccepted {
		t.Fatalf("Status = %q, want %q", a.Status, AckAccepted)
	}
}
