// Command sweep-cellgift watches the sweep fleet's status file and, when a
// sweep explorer docks at the cell depot station (Event Horizon Relay in
// Alzirr by default), queues a handoff: the resident holder there gifts it
// fuel cells from storage. The holder's worker fulfils the record on its next
// idle pass (HandoffPass); the explorer's sweep worker loads the gift into
// cargo while docked (sweepCollectCells). One gift per explorer per cooldown.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

// idPrefix marks this tool's handoff records; the sweep worker collects only these.
const idPrefix = "sweepcells/"

type config struct {
	Holder   string
	POI      string // station POI id, as the status file reports a docked ship
	Station  string // station base id, as the handoff record and DockedAtBase name it
	Item     string
	Qty      int
	Cooldown time.Duration
}

// plan returns the handoff records to enqueue: one per worker docked at the
// station that has free cargo and no gift of ours in the last Cooldown.
func plan(workers []balances.LiveRecord, existing []handoff.Record, now time.Time, cfg config) []handoff.Record {
	var out []handoff.Record
	for _, w := range workers {
		if !w.Docked || w.POI != cfg.POI || w.AgentID == cfg.Holder {
			continue
		}
		qty := min(cfg.Qty, int(w.CargoCapacity-w.CargoUsed))
		if qty < 1 || giftedRecently(existing, w.AgentID, now, cfg.Cooldown) {
			continue
		}
		out = append(out, handoff.Record{
			ID:        fmt.Sprintf("%s%s/%s", idPrefix, w.AgentID, now.UTC().Format("20060102T1504")),
			Holder:    cfg.Holder,
			Station:   cfg.Station,
			ItemID:    cfg.Item,
			Qty:       qty,
			Recipient: w.AgentID,
		})
	}
	return out
}

func giftedRecently(recs []handoff.Record, agent string, now time.Time, cooldown time.Duration) bool {
	for _, r := range recs {
		if r.Recipient != agent || !strings.HasPrefix(r.ID, idPrefix) {
			continue
		}
		at, err := time.Parse(time.RFC3339, r.UpdatedAt)
		if err != nil || now.Sub(at) < cooldown {
			return true
		}
	}
	return false
}

func main() {
	statusPath := flag.String("status", "data/overmind/sweep-status.json", "sweep fleet status file")
	queuePath := flag.String("queue", "data/overmind/handoff-queue.json", "shared handoff queue")
	holder := flag.String("holder", "marketbot_010", "agent holding the cells in its storage at the station")
	poi := flag.String("poi", "b6007ea0d1b905492a17dcd5d3e72286", "POI id of the depot station (Event Horizon Relay), matched against the status file")
	station := flag.String("station", "2e8caabbf604606db0692b3f71751ded", "base id of the depot station (Event Horizon Relay), written to the handoff record")
	item := flag.String("item", "fuel_cell", "item to gift")
	qty := flag.Int("qty", 40, "units per gift (capped at the recipient's free cargo)")
	cooldown := flag.Duration("cooldown", 12*time.Hour, "minimum time between gifts to one explorer")
	once := flag.Bool("once", false, "run one pass and exit")
	flag.Parse()

	cfg := config{Holder: *holder, POI: *poi, Station: *station, Item: *item, Qty: *qty, Cooldown: *cooldown}
	q := handoff.NewQueue(*queuePath)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		if err := pass(*statusPath, q, cfg); err != nil {
			log.Printf("sweep-cellgift: %v", err)
		}
		if *once {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(game.SleepLong):
		}
	}
}

func pass(statusPath string, q *handoff.Queue, cfg config) error {
	sf, err := balances.ReadStatus(statusPath)
	if err != nil {
		return err
	}
	recs, err := q.List()
	if err != nil {
		return fmt.Errorf("read queue: %w", err)
	}
	for _, rec := range plan(sf.Workers, recs, time.Now(), cfg) {
		ok, err := q.Enqueue(rec)
		if err != nil {
			return fmt.Errorf("enqueue %s: %w", rec.ID, err)
		}
		if ok {
			log.Printf("sweep-cellgift: queued %d %s %s -> %s at %s", rec.Qty, rec.ItemID, rec.Holder, rec.Recipient, rec.Station)
		}
	}
	return nil
}
