package worker

import (
	"slices"
	"strings"
	"sync"
)

// The KB keeps POI rows the server has since renamed or removed (the October
// 2026 themed renames left hundreds). Such a row still ranks as a hunting
// ground — often first, since nobody has mined it since — and travel to it is
// refused with "Unknown destination: <id>". A hunter that only ended the pass
// there picked the same dead id again on the next one, forever.
//
// huntGone remembers the ids the server has disowned for the life of the
// worker process, so each costs at most one wasted leg per restart. The KB row
// is left alone: its poi_resources still describe the renamed site.
var huntGone = &huntGoneSet{}

type huntGoneSet struct {
	mu  sync.Mutex
	ids []string
}

func (g *huntGoneSet) add(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !slices.Contains(g.ids, id) {
		g.ids = append(g.ids, id)
	}
}

func (g *huntGoneSet) list() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.ids)
}

func (g *huntGoneSet) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ids = nil
}

// huntGoneGround returns the POI id the server refused as an unknown
// destination, or "" when err is anything else. The server names the id
// itself: `Unknown destination: smelt_reef_frostfeld`.
func huntGoneGround(err error) string {
	if !isUnknownDestinationErr(err) {
		return ""
	}
	msg := err.Error()
	i := strings.LastIndex(strings.ToLower(msg), unknownDestinationMarker)
	rest := strings.TrimLeft(msg[i+len(unknownDestinationMarker):], ": ")
	if end := strings.IndexAny(rest, " ;,\n"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}
