package main

import (
	"fmt"

	"github.com/peterh/liner"
	"github.com/rsned/spacemolt/pkg/worker"
)

// readLogicalCommand reads a command from liner. If the first line has
// unbalanced '{' at top level (outside quotes), it continues reading
// additional lines with a "... " prompt until brace depth returns to 0,
// joining lines with '\n'. Returns the assembled script. On Ctrl-C
// during continuation, returns (combined-so-far, liner.ErrPromptAborted).
func readLogicalCommand(line *liner.State) (string, error) {
	first, err := line.Prompt("$ ")
	if err != nil {
		return "", err
	}
	depth, inQuote := worker.ScanBraceDepth(first)
	if depth <= 0 && !inQuote {
		return first, nil
	}
	combined := first
	for depth > 0 || inQuote {
		more, perr := line.Prompt("... ")
		if perr != nil {
			return combined, perr
		}
		combined += "\n" + more
		depth, inQuote = worker.ScanBraceDepth(combined)
		if depth < 0 {
			return combined, fmt.Errorf("unbalanced braces")
		}
	}
	return combined, nil
}
