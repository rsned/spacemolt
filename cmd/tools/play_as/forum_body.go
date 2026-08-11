package main

import (
	"fmt"
	"os"
	"strings"
)

// forumBody resolves the text of a forum post from either
//
//	forum_reply <thread-id> some words here
//	forum_reply <thread-id> --file=<path>
//
// args is the part of the command line AFTER the fixed positional arguments.
//
// The inline form joins tokens with a single space, which is all the REPL can
// ever give it: the reader splits a line on whitespace, so a newline cannot
// survive to reach the server and a multi-paragraph post collapses into one
// run-on line. Forum posts are the one thing play_as sends that a human reads,
// and a bug report worth writing is worth paragraphing — hence --file, which
// is sent byte for byte.
//
// --file is deliberately spelled the same way `craft --file` is.
func forumBody(args []string) (string, error) {
	for i, a := range args {
		path := ""
		switch {
		case strings.HasPrefix(a, "--file="):
			path = strings.TrimPrefix(a, "--file=")
		case a == "--file" && i+1 < len(args):
			path = args[i+1]
		default:
			continue
		}
		if path == "" {
			return "", fmt.Errorf("--file needs a path")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read post body %q: %w", path, err)
		}
		// Trailing newlines are an artefact of writing the file, not part of
		// what the author meant to post.
		body := strings.TrimRight(string(b), "\n")
		if strings.TrimSpace(body) == "" {
			return "", fmt.Errorf("post body %q is empty", path)
		}

		return body, nil
	}

	body := strings.Join(args, " ")
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("a post needs some text (or --file=<path>)")
	}

	return body, nil
}
