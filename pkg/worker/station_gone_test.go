package worker

import (
	"errors"
	"path/filepath"
	"testing"
)

// A player station can be DISMANTLED, and the knowledge base keeps serving its
// POI row long after: `pois` is only refreshed when an agent happens to visit
// that system, so a station in a system nobody has flown to in weeks reads as
// perfectly good data. The 2026-08-12 survey flew seven jumps to Veilwatch
// Shoal in oakridge -- last seen at tick 1,429,376 against a clock of
// 1,599,600 -- and got `invalid_poi: Unknown destination`. ENDL Kitalpha Cache
// answered the same way three jumps later. Both were the two stalest rows in
// the table.
//
// That answer is CONCLUSIVE and was being discarded as an ordinary transit
// failure, so every future pass would fly the same fuel at the same ghost.

const goneTestStation = "c1d7f0addf701eb00aae3518a8ba6405"

func TestRecordTransitLearnsAStationIsGone(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))

	a.RecordTransit(goneTestStation, errors.New("travel to c1d7f0addf701eb00aae3518a8ba6405 failed: Unknown destination: c1d7f0addf701eb00aae3518a8ba6405"))

	if !a.Gone(goneTestStation) {
		t.Fatal("an `Unknown destination` transit error must prove the station is gone")
	}
}

func TestRecordTransitMatchesTheInvalidPOICode(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))

	a.RecordTransit(goneTestStation, errors.New(`server said {"code":"invalid_poi"}`))

	if !a.Gone(goneTestStation) {
		t.Fatal("the invalid_poi code must prove the station is gone")
	}
}

// The whole discipline of this map is that only conclusive answers teach it.
// A disconnect mid-jump, a route that could not be planned, a timeout -- none
// of those say anything about whether the station is still standing, and
// treating them as proof would erase good stations permanently.
func TestRecordTransitIgnoresInconclusiveFailures(t *testing.T) {
	for _, msg := range []string{
		"websocket: close 1006 (abnormal closure)",
		"no route to system",
		"context deadline exceeded",
		"Access denied",
	} {
		a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))
		a.RecordTransit(goneTestStation, errors.New(msg))
		if a.Gone(goneTestStation) {
			t.Fatalf("%q is not evidence the station is gone", msg)
		}
	}
}

// Gone and denied are different facts about different worlds: one station
// turned us away, the other is not there to turn anyone away. An operator
// reading this file to find out why a fare was refused must be able to tell
// them apart, so a refusal must never land in the gone set.
func TestGoneIsNotDenied(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))

	a.RecordTransit(goneTestStation, errors.New("Unknown destination: x"))

	if a.Denied(goneTestStation) {
		t.Fatal("a dismantled station must not be recorded as having refused us")
	}
}

// The dangerous case: a station we PROVED open, later dismantled. `open` stays
// true forever by design (access is evidence that stands), so Deliverable must
// consult the gone set or it will keep booking fares to a station that is not
// there.
func TestAProvenOpenStationThatVanishesBecomesUndeliverable(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))
	a.RecordDock(goneTestStation, nil)
	if !a.Deliverable(goneTestStation) {
		t.Fatal("precondition: a clean dock must prove the station deliverable")
	}

	a.RecordTransit(goneTestStation, errors.New("Unknown destination: x"))

	if a.Deliverable(goneTestStation) {
		t.Fatal("a station that has vanished must not remain deliverable")
	}
}

// Symmetric to the rest of the map: arriving proves it exists. A station id can
// be reused or a structure rebuilt, and a successful transit is the same grade
// of evidence going the other way.
func TestReachingAStationClearsTheGoneMark(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))
	a.RecordTransit(goneTestStation, errors.New("Unknown destination: x"))

	a.RecordTransit(goneTestStation, nil)

	if a.Gone(goneTestStation) {
		t.Fatal("arriving at a station must clear a stale gone mark")
	}
}

func TestGoneSurvivesAReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")
	a := LoadStationAccess(path)
	a.RecordTransit(goneTestStation, errors.New("Unknown destination: x"))

	if !LoadStationAccess(path).Gone(goneTestStation) {
		t.Fatal("the gone set must persist; the point is to stop re-flying the same ghost")
	}
}

// NPC stations are addressed by readable slug ids that the server has always
// known. An `Unknown destination` against one of those is a routing bug in our
// own code, not a dismantled station, and recording it would hide the bug
// behind a permanent skip.
func TestRecordTransitIgnoresNPCStations(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))

	a.RecordTransit("treasure_cache_trading_post", errors.New("Unknown destination: treasure_cache_trading_post"))

	if a.Gone("treasure_cache_trading_post") {
		t.Fatal("an NPC station id must not be recorded as gone")
	}
}
