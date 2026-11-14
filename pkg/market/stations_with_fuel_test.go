package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStationsWithFuel(t *testing.T) {
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	at := time.Now().UTC().Format(time.RFC3339)
	for _, s := range []StationFuel{
		{StationID: "full", FuelReserve: 50000, ReserveObservedAt: at, CapturedAt: at},
		{StationID: "dry", FuelReserve: 0, ReserveObservedAt: at, CapturedAt: at},
		{StationID: "unknown", CapturedAt: at}, // price only, reserve never observed
	} {
		if uerr := c.UpsertStationFuel(ctx, s); uerr != nil {
			t.Fatal(uerr)
		}
	}
	got, err := c.StationsWithFuel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got["full"] || got["dry"] || got["unknown"] || len(got) != 1 {
		t.Fatalf("got %v, want only full", got)
	}
}
