package craftbrain

import (
	"testing"
	"time"
)

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if o.MaxHandTicks != 360 {
		t.Errorf("MaxHandTicks = %v, want 360", o.MaxHandTicks)
	}
	if o.MaxStockAge != 24*time.Hour {
		t.Errorf("MaxStockAge = %v, want 24h", o.MaxStockAge)
	}
}

func TestFacilityParseProduction(t *testing.T) {
	var f Facility
	const details = `{"production":{"ticks_per_run":1.2173913,"output_per_run":2,"backlog_ticks":7,"rental_fee_per_run":350}}`
	if err := f.ParseProduction(details); err != nil {
		t.Fatalf("ParseProduction: %v", err)
	}
	if f.TicksPerRun != 1.2173913 {
		t.Errorf("TicksPerRun = %v, want 1.2173913", f.TicksPerRun)
	}
	if f.OutputPerRun != 2 {
		t.Errorf("OutputPerRun = %d, want 2", f.OutputPerRun)
	}
	if f.BacklogTicks != 7 {
		t.Errorf("BacklogTicks = %v, want 7", f.BacklogTicks)
	}
}

// A facility we have never observed carries no details; OutputPerRun must
// default to 1 rather than 0, or runs = ceil(n/0) divides by zero.
func TestFacilityParseProduction_EmptyDefaultsOutputToOne(t *testing.T) {
	var f Facility
	if err := f.ParseProduction(""); err != nil {
		t.Fatalf("ParseProduction(empty): %v", err)
	}
	if f.OutputPerRun != 1 {
		t.Errorf("OutputPerRun = %d, want 1", f.OutputPerRun)
	}
}

func TestNewEngineRequiresSource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) must panic")
		}
	}()
	_ = New(nil)
}
