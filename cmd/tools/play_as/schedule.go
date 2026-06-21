package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/worker"
)

// handleScheduleAdd parses `schedule_add <freq> <command...>`, registers the
// task, and (run-immediately) executes the command once via runner.
func handleScheduleAdd(s *worker.Scheduler, runner func(string), now time.Time, args []string) {
	if len(args) < 2 {
		fmt.Println("usage: schedule_add <hourly|daily|weekly> <command...>")
		return
	}
	freq := strings.ToLower(args[0])
	command := strings.Join(args[1:], " ")

	// Don't let the scheduler schedule its own management commands.
	if cmdName := strings.ToLower(args[1]); strings.HasPrefix(cmdName, "schedule_") || cmdName == "view_scheduled" {
		fmt.Printf("refusing to schedule the %q builtin\n", args[1])
		return
	}

	task, err := s.Add(freq, command, now)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("scheduled #%d (%s): %s\n", task.ID, task.Frequency, task.Command)
	fmt.Printf("  next due %s; running now…\n", worker.NextBoundary(task.Frequency, now).Format("2006-01-02 15:04 UTC"))
	runner(command)
}

// handleScheduleRemove parses `schedule_remove <id>`.
func handleScheduleRemove(s *worker.Scheduler, args []string) {
	if len(args) < 1 {
		fmt.Println("usage: schedule_remove <id>")
		return
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Printf("invalid id %q\n", args[0])
		return
	}
	if s.Remove(id) {
		fmt.Printf("removed scheduled task #%d\n", id)
	} else {
		fmt.Printf("no scheduled task #%d\n", id)
	}
}

// handleViewScheduled prints the scheduled tasks as a table.
func handleViewScheduled(s *worker.Scheduler, now time.Time) {
	tasks := s.List()
	if len(tasks) == 0 {
		fmt.Println("  (no scheduled tasks)")
		return
	}
	fmt.Printf("  %-3s | %-7s | %-16s | %-16s | %s\n", "ID", "Freq", "Next Due (UTC)", "Last Run (UTC)", "Command")
	fmt.Printf("  %s-+-%s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", 3), strings.Repeat("-", 7), strings.Repeat("-", 16),
		strings.Repeat("-", 16), strings.Repeat("-", 20))
	for _, t := range tasks {
		last := "never"
		if !t.LastRun.IsZero() {
			last = t.LastRun.UTC().Format("01-02 15:04")
		}
		next := worker.NextBoundary(t.Frequency, now).Format("01-02 15:04")
		fmt.Printf("  %-3d | %-7s | %-16s | %-16s | %s\n", t.ID, t.Frequency, next, last, t.Command)
	}
}
