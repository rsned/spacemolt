package game

import (
	"testing"
	"time"
)

// TestArrivalWaitTimeout pins the safety-bound semantics of the travel/jump
// arrival wait. The local GameClock can drift AHEAD of the server tick (its
// sync only moves forward), so arrivalTick-startTick can under-estimate and
// even collapse to <=0. The wait must never fall below the floor, and must
// never exceed the cap.
func TestArrivalWaitTimeout(t *testing.T) {
	const floor = 9 * SleepTick // 90s
	cases := []struct {
		name        string
		arrivalTick int64
		startTick   int64
		floor       time.Duration
		max         time.Duration
		want        time.Duration
	}{
		{
			// The reported bug: a 4-tick journey whose ticksRemaining collapsed
			// to 1 because the clock had drifted ~3 ticks ahead. Pre-fix this
			// produced 1*SleepTick+30s = 40s and timed out mid-travel.
			name: "drift collapses delta to one tick -> floored",
			arrivalTick: 104, startTick: 103,
			floor: floor, max: SleepTravelMaxWait, want: floor,
		},
		{
			name: "clock drifted past arrival (negative delta) -> floored",
			arrivalTick: 100, startTick: 110,
			floor: floor, max: SleepTravelMaxWait, want: floor,
		},
		{
			name: "short legit journey below floor -> floored",
			arrivalTick: 4, startTick: 0, // 4*10+30 = 70s < 90s
			floor: floor, max: SleepTravelMaxWait, want: floor,
		},
		{
			name: "mid journey above floor, below cap -> computed",
			arrivalTick: 10, startTick: 0, // 10*10+30 = 130s
			floor: floor, max: SleepTravelMaxWait, want: 130 * time.Second,
		},
		{
			name: "long journey exceeds travel cap -> capped",
			arrivalTick: 30, startTick: 0, // 30*10+30 = 330s > 180s cap
			floor: floor, max: SleepTravelMaxWait, want: SleepTravelMaxWait,
		},
		{
			name: "jump uses larger cap",
			arrivalTick: 40, startTick: 0, // 40*10+30 = 430s > 300s jump cap
			floor: floor, max: SleepJumpMaxWait, want: SleepJumpMaxWait,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := arrivalWaitTimeout(tc.arrivalTick, tc.startTick, tc.floor, tc.max)
			if got != tc.want {
				t.Errorf("arrivalWaitTimeout(%d, %d, %v, %v) = %v, want %v",
					tc.arrivalTick, tc.startTick, tc.floor, tc.max, got, tc.want)
			}
		})
	}
}
