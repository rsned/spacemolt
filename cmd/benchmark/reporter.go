package main

import (
	"cmp"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"time"
)

// Reporter handles periodic and final output of benchmark results.
type Reporter struct {
	metrics  *Metrics
	cfg      *Config
	logger   *log.Logger
	interval time.Duration
}

// NewReporter creates a reporter with a 5-second reporting interval.
func NewReporter(metrics *Metrics, cfg *Config, logger *log.Logger) *Reporter {
	return &Reporter{
		metrics:  metrics,
		cfg:      cfg,
		logger:   logger,
		interval: 5 * time.Second,
	}
}

// RunPeriodic prints a status line every interval until ctx is cancelled.
func (r *Reporter) RunPeriodic(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			snap := r.metrics.Snapshot()
			fmt.Printf("\r[%v] cmds=%d errs=%d(transport=%d rate=%d game=%d) cmd/s=%.1f p50=%v p99=%v reconn=%d\n",
				snap.Elapsed.Truncate(time.Second),
				snap.TotalCommands,
				snap.TotalErrors,
				snap.TransportErrs,
				snap.RateLimits,
				snap.GameErrors,
				snap.CommandsPerSec,
				snap.Overall.P50,
				snap.Overall.P99,
				snap.Reconnects,
			)
		case <-ctx.Done():
			return
		}
	}
}

// PrintFinal outputs the final summary.
func (r *Reporter) PrintFinal() {
	snap := r.metrics.Snapshot()

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("  BENCHMARK RESULTS")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("  Duration:        %v\n", snap.Elapsed.Truncate(time.Second))
	fmt.Printf("  Total commands:  %d\n", snap.TotalCommands)
	fmt.Printf("  Commands/sec:    %.2f\n", snap.CommandsPerSec)
	fmt.Printf("  Total errors:    %d (%.1f%%)\n", snap.TotalErrors, errorRate(snap.TotalErrors, snap.TotalCommands))
	fmt.Printf("    Transport:     %d\n", snap.TransportErrs)
	fmt.Printf("    Rate limits:   %d\n", snap.RateLimits)
	fmt.Printf("    Game logic:    %d\n", snap.GameErrors)
	fmt.Printf("  Reconnects:      %d\n", snap.Reconnects)
	fmt.Printf("  Disconnects:     %d\n", snap.Disconnects)
	fmt.Println()
	fmt.Println("  Overall Latency (successful commands):")
	printLatencyStats("    ", snap.Overall)
	fmt.Println()

	if len(snap.PerCommand) > 0 {
		fmt.Println("  Per-Command Latency:")
		for cmd, stats := range snap.PerCommand {
			fmt.Printf("    %s (n=%d):\n", cmd, stats.Count)
			printLatencyStats("      ", stats)
		}
	}

	// Error breakdown by code
	if len(snap.ErrorsByCode) > 0 {
		fmt.Println()
		fmt.Println("  Error Breakdown by Code:")
		codes := sortedErrorCodes(snap.ErrorsByCode)
		for _, ec := range codes {
			fmt.Printf("    %-20s %d\n", ec.code, ec.count)
		}
	}

	// Per-command error breakdown
	if len(snap.ErrorsByCommand) > 0 {
		fmt.Println()
		fmt.Println("  Errors by Command:")
		for cmd, ces := range snap.ErrorsByCommand {
			fmt.Printf("    %s: %d errors (transport=%d rate=%d game=%d)\n",
				cmd, ces.Total, ces.Transport, ces.RateLimit, ces.GameLogic)
			codes := sortedErrorCodes(ces.ByCodes)
			for _, ec := range codes {
				fmt.Printf("      %-20s %d\n", ec.code, ec.count)
			}
		}
	}

	fmt.Println("═══════════════════════════════════════════════════════")
}

// Export writes results to a file in the configured format.
func (r *Reporter) Export() error {
	if r.cfg.OutputFile == "" {
		return nil
	}

	snap := r.metrics.Snapshot()

	switch r.cfg.Output {
	case "json":
		return r.exportJSON(snap)
	case "csv":
		return r.exportCSV(snap)
	default:
		return nil
	}
}

func (r *Reporter) exportJSON(snap MetricsSnapshot) error {
	f, err := os.Create(r.cfg.OutputFile)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	output := map[string]any{
		"duration_seconds": snap.Elapsed.Seconds(),
		"total_commands":   snap.TotalCommands,
		"commands_per_sec": snap.CommandsPerSec,
		"errors": map[string]any{
			"total":      snap.TotalErrors,
			"transport":  snap.TransportErrs,
			"rate_limit": snap.RateLimits,
			"game_logic": snap.GameErrors,
			"by_code":    snap.ErrorsByCode,
			"by_command": snap.ErrorsByCommand,
		},
		"reconnects":  snap.Reconnects,
		"disconnects": snap.Disconnects,
		"latency":     latencyStatsMap(snap.Overall),
		"per_command":  perCommandMap(snap.PerCommand),
	}

	return enc.Encode(output)
}

func (r *Reporter) exportCSV(snap MetricsSnapshot) error {
	f, err := os.Create(r.cfg.OutputFile)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	return writeCSV(f, snap)
}

func writeCSV(w io.Writer, snap MetricsSnapshot) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{"command", "count", "avg_ms", "p50_ms", "p90_ms", "p99_ms", "min_ms", "max_ms"}
	if err := cw.Write(header); err != nil {
		return err
	}

	// Overall row
	if err := cw.Write(latencyCSVRow("_overall", snap.Overall)); err != nil {
		return err
	}

	// Per-command rows
	for cmd, stats := range snap.PerCommand {
		if err := cw.Write(latencyCSVRow(cmd, stats)); err != nil {
			return err
		}
	}

	return nil
}

func latencyCSVRow(cmd string, s LatencyStats) []string {
	return []string{
		cmd,
		fmt.Sprintf("%d", s.Count),
		fmt.Sprintf("%.2f", float64(s.Avg.Microseconds())/1000),
		fmt.Sprintf("%.2f", float64(s.P50.Microseconds())/1000),
		fmt.Sprintf("%.2f", float64(s.P90.Microseconds())/1000),
		fmt.Sprintf("%.2f", float64(s.P99.Microseconds())/1000),
		fmt.Sprintf("%.2f", float64(s.Min.Microseconds())/1000),
		fmt.Sprintf("%.2f", float64(s.Max.Microseconds())/1000),
	}
}

func printLatencyStats(prefix string, s LatencyStats) {
	if s.Count == 0 {
		fmt.Printf("%sNo data\n", prefix)
		return
	}
	fmt.Printf("%sCount: %d\n", prefix, s.Count)
	fmt.Printf("%sAvg:   %v\n", prefix, s.Avg)
	fmt.Printf("%sP50:   %v\n", prefix, s.P50)
	fmt.Printf("%sP90:   %v\n", prefix, s.P90)
	fmt.Printf("%sP99:   %v\n", prefix, s.P99)
	fmt.Printf("%sMin:   %v\n", prefix, s.Min)
	fmt.Printf("%sMax:   %v\n", prefix, s.Max)
}

func errorRate(errors, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(errors) / float64(total) * 100
}

func latencyStatsMap(s LatencyStats) map[string]any {
	return map[string]any{
		"count":  s.Count,
		"avg_ms": float64(s.Avg.Microseconds()) / 1000,
		"p50_ms": float64(s.P50.Microseconds()) / 1000,
		"p90_ms": float64(s.P90.Microseconds()) / 1000,
		"p99_ms": float64(s.P99.Microseconds()) / 1000,
		"min_ms": float64(s.Min.Microseconds()) / 1000,
		"max_ms": float64(s.Max.Microseconds()) / 1000,
	}
}

func perCommandMap(perCommand map[string]LatencyStats) map[string]any {
	result := make(map[string]any, len(perCommand))
	for cmd, stats := range perCommand {
		result[cmd] = latencyStatsMap(stats)
	}
	return result
}

type errorCodeCount struct {
	code  string
	count int64
}

// sortedErrorCodes returns error codes sorted by count descending.
func sortedErrorCodes(m map[string]int64) []errorCodeCount {
	out := make([]errorCodeCount, 0, len(m))
	for code, count := range m {
		out = append(out, errorCodeCount{code, count})
	}
	slices.SortFunc(out, func(a, b errorCodeCount) int {
		if a.count != b.count {
			return cmp.Compare(b.count, a.count) // descending
		}
		return cmp.Compare(a.code, b.code)
	})
	return out
}
