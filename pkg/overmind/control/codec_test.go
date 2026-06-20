package control

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	in := []Envelope{
		{Type: TypeHello, AgentID: "a"},
		{Type: TypeStatus, AgentID: "a"},
	}
	for _, e := range in {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	dec := NewDecoder(&buf)
	for i := range in {
		got, err := dec.Decode()
		if err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		if got.Type != in[i].Type || got.AgentID != in[i].AgentID {
			t.Fatalf("decode %d mismatch: %+v", i, got)
		}
	}
	if _, err := dec.Decode(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestEncoderConcurrentSafe(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = enc.Encode(Envelope{Type: TypeStatus, AgentID: "x"})
		}()
	}
	wg.Wait()
	dec := NewDecoder(&buf)
	n := 0
	for {
		if _, err := dec.Decode(); err != nil {
			break
		}
		n++
	}
	if n != 50 {
		t.Fatalf("expected 50 framed messages, got %d", n)
	}
}
