// Package explore is the per-system explore shared by play_as and the worker:
// survey the system, then visit every POI and capture what is there.
package explore

import (
	"fmt"
	"io"
)

// Reporter receives an explore's progress.
type Reporter interface {
	// Printf receives the human progress lines.
	Printf(format string, args ...any)
	// Raw receives each raw server reply, for callers that print JSON. label
	// is RawSurvey or RawExplore.
	Raw(label string, payload []byte)
}

// Labels passed to Reporter.Raw.
const (
	RawSurvey  = "survey"
	RawExplore = "explore"
)

type discard struct{}

func (discard) Printf(string, ...any) {}
func (discard) Raw(string, []byte)    {}

// WriterReporter writes the progress lines to w and drops raw replies.
type WriterReporter struct{ W io.Writer }

// Printf writes one progress line to the writer.
func (r WriterReporter) Printf(format string, args ...any) {
	if r.W != nil {
		fmt.Fprintf(r.W, format, args...) //nolint:errcheck
	}
}

// Raw drops the reply.
func (WriterReporter) Raw(string, []byte) {}
