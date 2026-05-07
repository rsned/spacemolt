package respfmt

import "strings"

// styledErrors maps (command, error-substring) pairs to friendly,
// command-specific messages. The substring match keeps the table
// resilient to small changes in server error wording.
var styledErrors = map[[2]string]string{
	{"mine", "depleted"}: "Ore depleted.",
}

// Error renders err as a single styled line.
//
// If a (command, substring) pair in the override table matches, the
// friendly message is returned. Otherwise the raw error text is prefixed
// with "Error: ".
//
// Callers that need transport-level context (e.g. "we're reconnecting,
// retry in a moment") should wrap this — Error makes no assumption about
// connection state. err must be non-nil; passing nil returns "".
func Error(err error, command string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for key, friendly := range styledErrors {
		if key[0] == command && strings.Contains(msg, key[1]) {
			return friendly
		}
	}
	return "Error: " + msg
}
