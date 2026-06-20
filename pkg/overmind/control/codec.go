package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// maxLineBytes bounds a single NDJSON line. Status/event payloads are small;
// 1 MiB is generous headroom against the 64 KiB bufio.Scanner default.
const maxLineBytes = 1 << 20

// Encoder writes length-delimited (newline) JSON envelopes. Safe for
// concurrent use by multiple goroutines.
type Encoder struct {
	mu sync.Mutex
	w  *bufio.Writer
}

// NewEncoder returns an Encoder writing to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriter(w)}
}

// Encode marshals env to one JSON line and flushes.
func (e *Encoder) Encode(env Envelope) error {
	raw, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("control: marshal envelope: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.w.Write(raw); err != nil {
		return fmt.Errorf("control: write: %w", err)
	}
	if err := e.w.WriteByte('\n'); err != nil {
		return fmt.Errorf("control: write newline: %w", err)
	}
	return e.w.Flush()
}

// Decoder reads newline-delimited JSON envelopes.
type Decoder struct {
	sc *bufio.Scanner
}

// NewDecoder returns a Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	return &Decoder{sc: sc}
}

// Decode reads the next envelope, returning io.EOF when the stream ends.
func (d *Decoder) Decode() (Envelope, error) {
	for d.sc.Scan() {
		line := d.sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var env Envelope
		if err := json.Unmarshal(line, &env); err != nil {
			return Envelope{}, fmt.Errorf("control: decode line: %w", err)
		}
		return env, nil
	}
	if err := d.sc.Err(); err != nil {
		return Envelope{}, fmt.Errorf("control: scan: %w", err)
	}
	return Envelope{}, io.EOF
}
