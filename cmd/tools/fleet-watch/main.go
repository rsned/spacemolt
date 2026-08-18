// Command fleet-watch watches the overmind fleets and the unsupervised daemons,
// and raises a desktop notification when something stops working.
//
// It answers one question: has the fleet stopped, as opposed to merely wobbling?
// The client disconnects and reconnects constantly under normal load (haul did
// 22 in half an hour, every one recovered), so the alerting rule is deliberately
// two-sided: a fleet is unhealthy when its log goes SILENT, or when disconnects
// go unmatched by reconnects across two consecutive passes. Either alone is
// noise; together they are the signature of the server actually being lost.
//
//	bin/fleet-watch --interval 60s --stale 3m --notify
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// tailBytes is how much of each log to read. The logs are multi-GIGABYTE and
// unrotated (mb-overmind.log alone is 7 GB), so this is always a seek to the end
// — never a scan. Big enough to hold several minutes of a busy fleet.
const tailBytes = 2 << 20

func main() {
	logsDir := flag.String("logs-dir", "data/overmind", "directory holding <fleet>-overmind.log")
	interval := flag.Duration("interval", time.Minute, "seconds between passes")
	stale := flag.Duration("stale", 3*time.Minute, "alert when a fleet's log has been silent this long")
	statusPath := flag.String("status-file", "data/overmind/fleet-watch-status.json", "current health snapshot")
	alertLog := flag.String("alert-log", "data/overmind/fleet-watch.log", "append alerts here")
	notify := flag.Bool("notify", true, "raise a desktop notification via notify-send")
	minWorkers := flag.Int("min-workers", 0, "alert when fewer workers than this are running (0 = derive from the first pass)")
	once := flag.Bool("once", false, "run a single pass and exit (for cron or a manual check)")
	fleetList := flag.String("fleets", "", "comma-separated fleets to watch (default: those with a running overmind)")
	flag.Parse()

	logger := log.New(os.Stderr, "[fleet-watch] ", log.LstdFlags)

	af, err := os.OpenFile(*alertLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Fatalf("open alert log: %v", err)
	}
	defer func() { _ = af.Close() }()

	watch := splitList(*fleetList)
	if len(watch) == 0 {
		watch = liveFleets()
		if len(watch) == 0 {
			logger.Fatalf("no running overmind found and no --fleets given; nothing to watch")
		}
	}
	// The set is fixed at startup on purpose. Deriving it every pass would make
	// a fleet vanish from the watch list at the exact moment its overmind died,
	// which is the event this exists to catch. Retired fleets leave their logs
	// behind (idle, mission, shuttle), so watching every *-overmind.log instead
	// would alert forever about fleets nobody is running.
	logger.Printf("watching fleets: %s", strings.Join(watch, ", "))

	w := &watcher{
		fleets:     watch,
		logsDir:    *logsDir,
		stale:      *stale,
		statusPath: *statusPath,
		alertFile:  af,
		notify:     *notify,
		minWorkers: *minWorkers,
		logger:     logger,
		prevUnrec:  map[string]int{},
		firing:     map[string]bool{},
	}

	if *once {
		w.pass()
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(*interval)
	defer tick.Stop()

	logger.Printf("watching %s every %s (stale after %s, notify=%v)", *logsDir, *interval, *stale, *notify)
	w.pass()
	for {
		select {
		case <-tick.C:
			w.pass()
		case <-stop:
			logger.Printf("shutting down")
			return
		}
	}
}

type watcher struct {
	fleets     []string
	logsDir    string
	stale      time.Duration
	statusPath string
	alertFile  io.Writer
	notify     bool
	minWorkers int
	logger     *log.Logger

	prevUnrec map[string]int
	// firing tracks which alerts are already active, so a persistent fault
	// notifies once rather than every pass. A watcher that cries every minute
	// gets muted, and a muted watcher is the same as no watcher.
	firing map[string]bool
}

type snapshot struct {
	CheckedAt string            `json:"checked_at"`
	Healthy   bool              `json:"healthy"`
	Fleets    map[string]fleetH `json:"fleets"`
	Processes map[string]int    `json:"processes"`
	Alerts    []string          `json:"alerts"`
}

type fleetH struct {
	LastLog     string `json:"last_log"`
	SilentFor   string `json:"silent_for"`
	Disconnects int    `json:"disconnects"`
	Reconnects  int    `json:"reconnects"`
}

func (w *watcher) pass() {
	now := time.Now()

	samples := w.sampleFleets()
	alerts, unrec := Evaluate(samples, w.prevUnrec, w.stale, now)
	w.prevUnrec = unrec

	counts := processCensus()
	if w.minWorkers == 0 && counts["worker"] > 0 {
		// Derive the floor from the first healthy observation, minus a little
		// slack for a worker mid-restart. Hard-coding 160 would go stale the
		// next time the roster changes.
		w.minWorkers = counts["worker"] - 3
		w.logger.Printf("worker floor set to %d (from %d running)", w.minWorkers, counts["worker"])
	}
	want := map[string]int{
		"overmind":          len(w.fleets),
		"arbitrage-scanner": 1,
		"market-prune":      1,
	}
	if w.minWorkers > 0 {
		want["worker"] = w.minWorkers
	}
	alerts = append(alerts, ProcessAlerts(counts, want)...)

	snap := snapshot{
		CheckedAt: now.Format(time.RFC3339),
		Healthy:   len(alerts) == 0,
		Fleets:    map[string]fleetH{},
		Processes: counts,
	}
	for _, s := range samples {
		h := fleetH{Disconnects: s.Disconnects, Reconnects: s.Reconnects}
		if s.HasStamp {
			h.LastLog = s.Newest.Format(time.RFC3339)
			h.SilentFor = now.Sub(s.Newest).Round(time.Second).String()
		}
		snap.Fleets[s.Fleet] = h
	}

	active := map[string]bool{}
	for _, a := range alerts {
		key := a.Fleet + "/" + a.Kind
		active[key] = true
		line := fmt.Sprintf("%s %s: %s", a.Fleet, a.Kind, a.Message)
		snap.Alerts = append(snap.Alerts, line)
		if !w.firing[key] {
			w.raise(now, line)
		}
	}
	// Announce recoveries too: a watcher that only ever reports bad news leaves
	// you unsure whether it is still running.
	for key := range w.firing {
		if !active[key] {
			w.raise(now, "RECOVERED "+strings.ReplaceAll(key, "/", " "))
		}
	}
	w.firing = active

	w.writeStatus(snap)
}

// raise records an alert and, when enabled, puts it on screen.
func (w *watcher) raise(now time.Time, line string) {
	stamped := fmt.Sprintf("%s %s\n", now.Format(time.RFC3339), line)
	if _, err := io.WriteString(w.alertFile, stamped); err != nil {
		w.logger.Printf("append alert: %v", err)
	}
	w.logger.Print(line)
	if !w.notify {
		return
	}
	// Best-effort: a headless session has no notification daemon, and that must
	// not stop the alert being logged.
	cmd := exec.Command("notify-send", "--urgency=critical", "Spacemolt fleet", line)
	if err := cmd.Run(); err != nil {
		w.logger.Printf("notify-send: %v", err)
	}
}

func (w *watcher) writeStatus(s snapshot) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		w.logger.Printf("marshal status: %v", err)
		return
	}
	tmp := w.statusPath + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		w.logger.Printf("write status: %v", err)
		return
	}
	if err := os.Rename(tmp, w.statusPath); err != nil {
		w.logger.Printf("rename status: %v", err)
	}
}

// sampleFleets reads the tail of each watched fleet's log.
func (w *watcher) sampleFleets() []Sample {
	out := make([]Sample, 0, len(w.fleets))
	for _, fleet := range w.fleets {
		p := filepath.Join(w.logsDir, fleet+"-overmind.log")
		tail, err := readTail(p, tailBytes)
		if err != nil {
			out = append(out, Sample{Fleet: fleet, ReadErr: err})
			continue
		}
		s := ParseTail(tail)
		s.Fleet = fleet
		out = append(out, s)
	}

	return out
}

// liveFleets names the fleets that have a running overmind, read from each
// supervisor's --socket argument.
//
// Processes are found by their resolved exe link, so this cannot match
// fleet-watch's own command line — the cmdline-matching trap that has already
// cost this project a stopped worker.
func liveFleets() []string {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil || filepath.Base(exe) != "overmind" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(string(raw), "\x00")
		for i, a := range args {
			if a == "--socket" && i+1 < len(args) {
				seen[strings.TrimSuffix(filepath.Base(args[i+1]), ".sock")] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)

	return out
}

// splitList parses a comma-separated flag into a trimmed, non-empty list.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}

	return out
}

// readTail returns the last n bytes of a file without reading the rest of it.
func readTail(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := st.Size() - n
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}

	return io.ReadAll(f)
}

// processCensus counts running processes by executable name.
//
// It matches on the resolved /proc/<pid>/exe symlink rather than the command
// line. A cmdline scan matches the watcher's OWN arguments — that trap has
// already cost this project a stopped worker — whereas the exe link cannot.
func processCensus() map[string]int {
	counts := map[string]int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return counts
	}
	interesting := map[string]bool{
		"overmind": true, "worker": true, "arbitrage-scanner": true,
		"market-prune": true, "overmind-status": true, "overmind-dashboard": true,
		"fleet-secondment": true,
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			continue
		}
		base := filepath.Base(exe)
		if interesting[base] {
			counts[base]++
		}
	}

	return counts
}
