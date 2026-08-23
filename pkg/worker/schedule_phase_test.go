package worker

import (
	"fmt"
	"testing"
	"time"
)

// The scheduler anchors every frequency to an absolute UTC grid, so a fleet of
// agents sharing a frequency fires in one instant. On 2026-08-23 that herd --
// 53 marketbots all issuing "refuel" at exactly 09:30:00 -- tripped the shared
// IP rate limiter and blocked every agent, escalating 229s -> 456s -> 911s.
// A per-agent phase keeps the once-per-period contract but staggers the wall
// clock each agent measures it against.

func TestBoundaryPhaseIsDeterministicAndBounded(t *testing.T) {
	for _, freq := range []string{"ten_minutely", "quarter_hourly", "half_hourly", "hourly", "daily", "weekly"} {
		first := BoundaryPhase("marketbot_sol", freq)
		if got := BoundaryPhase("marketbot_sol", freq); got != first {
			t.Errorf("%s: phase not deterministic: %v then %v", freq, first, got)
		}
		if first < 0 || first >= maxBoundaryPhase {
			t.Errorf("%s: phase %v outside [0,%v)", freq, first, maxBoundaryPhase)
		}
	}
}

func TestBoundaryPhaseSpreadsTheFleet(t *testing.T) {
	// 53 marketbots on the same frequency must not share a firing instant.
	buckets := map[time.Duration]int{}
	for i := range 53 {
		p := BoundaryPhase(fmt.Sprintf("marketbot_%03d", i), "ten_minutely")
		buckets[p.Truncate(10*time.Second)]++
	}
	// With 5 minutes of spread and 10s buckets there are 30 slots; a herd
	// would pile every agent into one.
	if len(buckets) < 15 {
		t.Errorf("phases clustered into %d buckets, want >=15 (herd not broken)", len(buckets))
	}
	for b, n := range buckets {
		if n > 12 {
			t.Errorf("bucket %v holds %d agents, want <=12", b, n)
		}
	}
}

func TestPhasedBoundaryStillFiresOncePerPeriod(t *testing.T) {
	// Walk a full hour a second at a time; a ten_minutely task must come due
	// exactly 6 times regardless of its phase.
	s := &Scheduler{phaseSeed: "marketbot_sol"}
	s.tasks = []ScheduledTask{{ID: 1, Frequency: "ten_minutely", Command: "view_market"}}
	start := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	s.tasks[0].LastRun = start

	fires := 0
	for sec := 1; sec <= 3600; sec++ {
		now := start.Add(time.Duration(sec) * time.Second)
		if due := s.checkDueNoPersist(now); len(due) > 0 {
			fires++
		}
	}
	if fires != 6 {
		t.Errorf("ten_minutely fired %d times in an hour, want 6", fires)
	}
}

func TestPhasedBoundaryShiftsTheFiringInstant(t *testing.T) {
	// Two agents with different phases must not come due in the same second.
	a := &Scheduler{phaseSeed: "marketbot_sol", tasks: []ScheduledTask{{ID: 1, Frequency: "ten_minutely"}}}
	b := &Scheduler{phaseSeed: "marketbot_009", tasks: []ScheduledTask{{ID: 1, Frequency: "ten_minutely"}}}
	if BoundaryPhase("marketbot_sol", "ten_minutely") == BoundaryPhase("marketbot_009", "ten_minutely") {
		t.Skip("seeds collided; pick different fixtures")
	}
	start := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	a.tasks[0].LastRun, b.tasks[0].LastRun = start, start

	var aFire, bFire int
	for sec := 1; sec <= 600; sec++ {
		now := start.Add(time.Duration(sec) * time.Second)
		if len(a.checkDueNoPersist(now)) > 0 {
			aFire = sec
		}
		if len(b.checkDueNoPersist(now)) > 0 {
			bFire = sec
		}
	}
	if aFire == 0 || bFire == 0 {
		t.Fatalf("expected both to fire within the period; got a=%d b=%d", aFire, bFire)
	}
	if aFire == bFire {
		t.Errorf("both agents fired at second %d — herd not broken", aFire)
	}
}

// checkDueNoPersist is checkDue without the disk write: these tests build
// Schedulers with no path, and only care which instant a task comes due.
func (s *Scheduler) checkDueNoPersist(now time.Time) []ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	due := s.dueLocked(now)
	for i := range s.tasks {
		for _, d := range due {
			if s.tasks[i].ID == d.ID {
				s.tasks[i].LastRun = now.UTC()
			}
		}
	}
	return due
}
