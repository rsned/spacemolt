package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// realReply is a get_action_log body built from entries measured verbatim in
// craftsman-1's log on 2026-08-17. The ids, prices and data shapes are the
// server's own.
const realReply = `{
 "entries": [
  {"id": 32992948, "event_type": "other.rent_paid", "category": "other",
   "summary": "Facility rent: 9 credits for Signal Relay",
   "data": {"base_id": "grand_exchange_station", "cost": 9, "cycles": 1, "facility": "Signal Relay"},
   "created_at": "2026-06-17T15:55:51Z"},
  {"id": 32405332, "event_type": "trading.exchange_fill", "category": "trading",
   "summary": "Buy order filled: 1x Nickel Ore @ 2 each",
   "data": {"item_id": "nickel_ore", "price": 2, "quantity": 1, "role": "buyer", "total": 2},
   "created_at": "2026-06-16T14:34:14Z"},
  {"id": 93083359, "event_type": "navigation.jumped", "category": "navigation",
   "summary": "Jumped from Pollux to Treasure Cache",
   "data": {"arrival_poi": "dim_fortune", "first_visit": false, "from_system": "pollux",
            "is_pathfinder": false, "to_system": "treasure_cache", "to_system_name": "Treasure Cache"},
   "created_at": "2026-08-17T09:47:29Z"},
  {"id": 84804600, "event_type": "combat.battle_ended", "category": "combat",
   "summary": "Battle ended in haven",
   "data": {"battle_id": "a48ff800a15380319f89e415f1d25cf0", "damage_dealt": 261,
            "damage_taken": 24, "duration": 4, "final_hull_pct": 100, "kills": 1,
            "outcome": "won", "reason": "victory", "system_id": "haven"},
   "created_at": "2026-08-10T00:04:43Z"}
 ],
 "has_more": true,
 "since_id": 1,
 "next_since_id": 93083359
}`

// TestActionLogFrom_RealReply pins the decode against a measured body.
func TestActionLogFrom_RealReply(t *testing.T) {
	page, ok, err := ActionLogFrom([]byte(realReply))
	if err != nil || !ok {
		t.Fatalf("ActionLogFrom: ok=%v err=%v", ok, err)
	}
	if len(page.Events) != 4 {
		t.Fatalf("events = %d, want 4", len(page.Events))
	}
	if page.NextSinceID != 93083359 {
		t.Errorf("NextSinceID = %d, want 93083359", page.NextSinceID)
	}
	if !page.HasMore {
		t.Error("HasMore = false, want true")
	}

	byID := map[int64]ActionLogEvent{}
	for _, e := range page.Events {
		byID[e.EventID] = e
	}

	rent := byID[32992948]
	if rent.Category != "other" || rent.EventType != "other.rent_paid" {
		t.Errorf("rent category/type = %q/%q", rent.Category, rent.EventType)
	}
	if rent.Data["cost"] != "9" || rent.Data["facility"] != "Signal Relay" {
		t.Errorf("rent data = %v", rent.Data)
	}

	// A bool must not become "%!s(bool=false)" or vanish.
	jump := byID[93083359]
	if jump.Data["first_visit"] != "false" || jump.Data["from_system"] != "pollux" {
		t.Errorf("jump data = %v", jump.Data)
	}

	// The forensic row: every field that identifies the battle survives.
	battle := byID[84804600]
	for k, want := range map[string]string{
		"battle_id": "a48ff800a15380319f89e415f1d25cf0",
		"outcome":   "won",
		"kills":     "1",
		"system_id": "haven",
	} {
		if battle.Data[k] != want {
			t.Errorf("battle data[%s] = %q, want %q", k, battle.Data[k], want)
		}
	}
}

// TestActionLogFrom_LargeNumbersKeepTheirDigits pins the digits of every number
// that reaches storage. Two distinct failure modes, both measured:
//
//   - Without UseNumber, a value inside data is a float64 before anything
//     formats it, and an integer past 2^53 has already lost its last digit:
//     9007199254740993 arrives as ...992 and no formatting can recover it.
//   - Rendering a decoded number with %v produces exponent form — 1200000
//     becomes "1.2e+06" — which is why nothing in actionLogValueString uses it.
//     This matters because %v is the obvious way to write that function.
//
// The event id is safe by construction: it is decoded through a json.Number
// field rather than out of the any-map, so it is never a float64.
func TestActionLogFrom_LargeNumbersKeepTheirDigits(t *testing.T) {
	raw := `{"entries":[{"id":9007199254740993,"event_type":"trading.exchange_fill",
	  "created_at":"2026-08-17T09:00:00Z",
	  "data":{"total":1200000,"price":600000,"quantity":2,"ratio":0.5,
	          "order_seq":9007199254740993}}],
	  "has_more":false,"next_since_id":9007199254740993}`

	page, ok, err := ActionLogFrom([]byte(raw))
	if err != nil || !ok {
		t.Fatalf("ActionLogFrom: ok=%v err=%v", ok, err)
	}
	e := page.Events[0]
	if e.EventID != 9007199254740993 {
		t.Errorf("EventID = %d, want 9007199254740993", e.EventID)
	}
	for k, want := range map[string]string{
		"total":    "1200000",
		"price":    "600000",
		"quantity": "2",
		"ratio":    "0.5",
		// The float64 round trip renders this as ...992.
		"order_seq": "9007199254740993",
	} {
		if got := e.Data[k]; got != want {
			t.Errorf("data[%s] = %q, want %q", k, got, want)
		}
	}
	for k, v := range e.Data {
		if strings.Contains(v, "e+") || strings.Contains(v, "e-") {
			t.Errorf("data[%s] = %q is in exponent form", k, v)
		}
	}
}

// TestActionLogCategory covers the prefix rule and both fallbacks.
func TestActionLogCategory(t *testing.T) {
	for _, tc := range []struct{ eventType, reply, want string }{
		{"combat.ship_destroyed", "combat", "combat"},
		{"tax.empire_levied", "", "tax"},       // undocumented category, prefix still works
		{"legacy_event", "session", "session"}, // dotless: fall back to the reply
		{"legacy_event", "", ""},               // dotless with nothing to fall back to
		{".leading_dot", "trading", "trading"}, // empty prefix is not a category
	} {
		if got := actionLogCategory(tc.eventType, tc.reply); got != tc.want {
			t.Errorf("actionLogCategory(%q,%q) = %q, want %q", tc.eventType, tc.reply, got, tc.want)
		}
	}
}

// TestActionLogFrom_NestedValuesSurvive keeps a structured value reconstructible
// instead of stringified into something unparseable.
func TestActionLogFrom_NestedValuesSurvive(t *testing.T) {
	raw := `{"entries":[{"id":5,"event_type":"mission.accepted","created_at":"2026-08-17T09:00:00Z",
	  "data":{"objectives":[{"item":"copper_ore","qty":10}],"reward":null,"nested":{"a":1}}}]}`
	page, _, err := ActionLogFrom([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	d := page.Events[0].Data
	if d["reward"] != "" {
		t.Errorf("null became %q, want empty", d["reward"])
	}
	var objs []map[string]any
	if err := json.Unmarshal([]byte(d["objectives"]), &objs); err != nil {
		t.Errorf("objectives not valid JSON: %q (%v)", d["objectives"], err)
	}
	if d["nested"] != `{"a":1}` {
		t.Errorf("nested = %q", d["nested"])
	}
}

// TestActionLogFrom_EmptyBody mirrors HullsFrom: a missing cache entry is "not
// captured", never an error and never an empty result treated as real.
func TestActionLogFrom_EmptyBody(t *testing.T) {
	if _, ok, err := ActionLogFrom(nil); ok || err != nil {
		t.Errorf("nil body: ok=%v err=%v, want false/nil", ok, err)
	}
}

// TestActionLogFrom_UnusableIDIsDropped: an entry with no id cannot be keyed or
// resumed from, so it must not be stored under a made-up one.
func TestActionLogFrom_UnusableIDIsDropped(t *testing.T) {
	raw := `{"entries":[{"id":0,"event_type":"x.y"},{"id":7,"event_type":"a.b"}]}`
	page, _, err := ActionLogFrom([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].EventID != 7 {
		t.Errorf("events = %+v, want only id 7", page.Events)
	}
}

func TestInsertActionLogEvents_Idempotent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	page, _, err := ActionLogFrom([]byte(realReply))
	if err != nil {
		t.Fatal(err)
	}

	n, err := st.InsertActionLogEvents(ctx, "p1", page.Events)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("first insert = %d, want 4", n)
	}
	// A re-walk after a cursor reset must not duplicate or error.
	n, err = st.InsertActionLogEvents(ctx, "p1", page.Events)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("re-insert = %d, want 0", n)
	}

	total, err := st.CountActionLogEvents(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Errorf("stored = %d, want 4", total)
	}

	// The same ids under another player are that player's own rows.
	if _, err := st.InsertActionLogEvents(ctx, "p2", page.Events); err != nil {
		t.Fatal(err)
	}
	if total, _ := st.CountActionLogEvents(ctx, "p2"); total != 4 {
		t.Errorf("p2 stored = %d, want 4", total)
	}
}

// TestActionLogEventsByType_RoundTripsData is the death-forensics read.
func TestActionLogEventsByType_RoundTripsData(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	page, _, _ := ActionLogFrom([]byte(realReply))
	if _, err := st.InsertActionLogEvents(ctx, "p1", page.Events); err != nil {
		t.Fatal(err)
	}

	got, err := st.ActionLogEventsByType(ctx, "p1", []string{"combat.battle_ended"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	if got[0].Data["battle_id"] != "a48ff800a15380319f89e415f1d25cf0" {
		t.Errorf("data did not survive the round trip: %v", got[0].Data)
	}

	all, err := st.ActionLogEventsByType(ctx, "p1", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Errorf("unfiltered rows = %d, want 4", len(all))
	}
	// Newest id first.
	if all[0].EventID != 93083359 {
		t.Errorf("first row = %d, want the highest id", all[0].EventID)
	}
}

func TestActionLogCursor_RoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

	if _, ok, err := st.LoadActionLogCursor(ctx, "p1"); ok || err != nil {
		t.Fatalf("unpolled agent: ok=%v err=%v, want false/nil", ok, err)
	}
	want := ActionLogCursor{NextSinceID: 93083359, EventsStored: 4, CaughtUp: true}
	if err := st.SaveActionLogCursor(ctx, "p1", want, now); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.LoadActionLogCursor(ctx, "p1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if got.NextSinceID != want.NextSinceID || got.EventsStored != want.EventsStored || !got.CaughtUp {
		t.Errorf("cursor = %+v, want %+v", got, want)
	}
}

// TestPruneActionLog_KeepsWhatTheForensicsNeed is the retention policy's
// contract: the bulk types age out, the cargo record does not.
func TestPruneActionLog_KeepsWhatTheForensicsNeed(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	fresh := now.Add(-1 * time.Hour).Format(time.RFC3339)

	evs := []ActionLogEvent{
		{EventID: 1, EventType: "navigation.jumped", CreatedAt: old},
		{EventID: 2, EventType: "ship.refuel", CreatedAt: old},
		{EventID: 3, EventType: "session.login", CreatedAt: old},
		{EventID: 4, EventType: "trading.buy_order_created", CreatedAt: old},
		// Kept: still inside the window.
		{EventID: 5, EventType: "navigation.jumped", CreatedAt: fresh},
		// Kept forever: this is the cargo manifest behind a loss.
		{EventID: 6, EventType: "trading.exchange_fill", CreatedAt: old},
		{EventID: 7, EventType: "combat.ship_destroyed", CreatedAt: old},
	}
	if _, err := st.InsertActionLogEvents(ctx, "p1", evs); err != nil {
		t.Fatal(err)
	}

	removed, err := st.PruneActionLog(ctx, "p1", now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 4 {
		t.Errorf("removed = %d, want 4", removed)
	}

	left := map[int64]bool{}
	rows, err := st.ActionLogEventsByType(ctx, "p1", nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		left[r.EventID] = true
	}
	for _, id := range []int64{5, 6, 7} {
		if !left[id] {
			t.Errorf("event %d was pruned but must be kept", id)
		}
	}
	for _, id := range []int64{1, 2, 3, 4} {
		if left[id] {
			t.Errorf("event %d survived the short TTL", id)
		}
	}

	// Idempotent: a second pass has nothing left to take.
	if removed, err := st.PruneActionLog(ctx, "p1", now); err != nil || removed != 0 {
		t.Errorf("second prune removed %d (err %v), want 0", removed, err)
	}
}

// TestPruneActionLog_DownsamplesRentToOnePerDay: rent_paid is a third of the
// busiest log measured, and only its daily figure is ever queried — so a day
// collapses to one row rather than disappearing.
func TestPruneActionLog_DownsamplesRentToOnePerDay(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	var evs []ActionLogEvent
	id := int64(1)
	for _, day := range []string{"2026-08-01", "2026-08-02"} {
		for h := range 4 {
			evs = append(evs, ActionLogEvent{
				EventID:   id,
				EventType: "other.rent_paid",
				CreatedAt: fmt.Sprintf("%sT0%d:00:00Z", day, h),
				Data:      map[string]string{"cost": "9"},
			})
			id++
		}
	}
	// Inside the window: untouched regardless of density.
	for h := range 3 {
		evs = append(evs, ActionLogEvent{
			EventID:   id,
			EventType: "other.rent_paid",
			CreatedAt: now.Add(-time.Duration(h) * time.Hour).Format(time.RFC3339),
		})
		id++
	}
	if _, err := st.InsertActionLogEvents(ctx, "p1", evs); err != nil {
		t.Fatal(err)
	}

	if _, err := st.PruneActionLog(ctx, "p1", now); err != nil {
		t.Fatal(err)
	}

	rows, err := st.ActionLogEventsByType(ctx, "p1", []string{"other.rent_paid"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	perDay := map[string]int{}
	for _, r := range rows {
		perDay[r.CreatedAt[:10]]++
	}
	for _, day := range []string{"2026-08-01", "2026-08-02"} {
		if perDay[day] != 1 {
			t.Errorf("%s kept %d rows, want 1", day, perDay[day])
		}
	}
	if perDay["2026-08-17"] != 3 {
		t.Errorf("today kept %d rows, want all 3", perDay["2026-08-17"])
	}
	// The survivor is the newest of its day, not an arbitrary one.
	for _, r := range rows {
		if r.CreatedAt[:10] == "2026-08-01" && r.CreatedAt != "2026-08-01T03:00:00Z" {
			t.Errorf("kept %s, want the day's newest row", r.CreatedAt)
		}
	}
}

// TestPruneActionLog_UndatedRowsSurvive: an entry with no created_at sorts below
// every cutoff, so a naive comparison would delete it on the first pass.
func TestPruneActionLog_UndatedRowsSurvive(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	if _, err := st.InsertActionLogEvents(ctx, "p1", []ActionLogEvent{
		{EventID: 1, EventType: "navigation.jumped", CreatedAt: ""},
		{EventID: 2, EventType: "other.rent_paid", CreatedAt: ""},
	}); err != nil {
		t.Fatal(err)
	}
	if removed, err := st.PruneActionLog(ctx, "p1", now); err != nil || removed != 0 {
		t.Errorf("removed %d undated rows (err %v), want 0", removed, err)
	}
}

// actionLogClient serves a scripted sequence of get_action_log replies and
// records the since_id each poll asked for.
type actionLogClient struct {
	game.GameClient
	state   *game.State
	replies []string
	polls   []int64
	err     error
}

func (c *actionLogClient) GetActionLog(_ context.Context, payload map[string]any) error {
	if c.err != nil {
		return c.err
	}
	switch v := payload["since_id"].(type) {
	case int64:
		c.polls = append(c.polls, v)
	case int:
		c.polls = append(c.polls, int64(v))
	}

	return nil
}

func (c *actionLogClient) GetRawJSON(string) []byte {
	i := len(c.polls) - 1
	if i < 0 || i >= len(c.replies) {
		return nil
	}

	return []byte(c.replies[i])
}

func (c *actionLogClient) GetState() *game.State { return c.state }

func (c *actionLogClient) GetStatus(context.Context) error { return nil }

func newActionLogClient(replies ...string) *actionLogClient {
	st := &game.State{}
	st.Player.ID = "p1"

	return &actionLogClient{state: st, replies: replies}
}

// fullPage renders a page_size-sized reply so the walk keeps going, with ids in
// [from, from+actionLogPageSize).
func fullPage(from int64) string {
	var b strings.Builder
	b.WriteString(`{"entries":[`)
	for i := range actionLogPageSize {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d,"event_type":"trading.exchange_fill","created_at":"2026-08-17T09:00:00Z","data":{"price":%d}}`,
			from+int64(i), i)
	}
	fmt.Fprintf(&b, `],"has_more":true,"next_since_id":%d}`, from+int64(actionLogPageSize)-1)

	return b.String()
}

// TestCaptureActionLog_WalksForwardAndStopsWhenCurrent covers the steady state:
// each poll carries the previous reply's next_since_id, and a short page ends
// the pass with the cursor marked caught up.
func TestCaptureActionLog_WalksForwardAndStopsWhenCurrent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	short := `{"entries":[{"id":300,"event_type":"skill.level_up","created_at":"2026-08-17T09:00:00Z","data":{"new_level":17}}],"has_more":false,"next_since_id":300}`
	c := newActionLogClient(fullPage(100), fullPage(200), short)

	res, err := CaptureActionLog(ctx, c, st, "craftsman-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Polls != 3 {
		t.Errorf("polls = %d, want 3", res.Polls)
	}
	if res.Inserted != 2*actionLogPageSize+1 {
		t.Errorf("inserted = %d, want %d", res.Inserted, 2*actionLogPageSize+1)
	}
	if !res.CaughtUp {
		t.Error("CaughtUp = false after a short page")
	}

	// The first poll must not send since_id=0 — the server documents 0 as
	// "normal newest-first paging", which would return the most recent page and
	// leave the walk unable to advance. Asserted as the literal 1 rather than
	// against actionLogFirstSinceID: comparing a constant to itself would pass
	// no matter what the constant was changed to.
	if len(c.polls) == 0 || c.polls[0] != 1 {
		t.Errorf("first poll since_id = %v, want 1", c.polls)
	}
	if c.polls[1] != 199 || c.polls[2] != 299 {
		t.Errorf("polls = %v, want the previous reply's next_since_id each time", c.polls)
	}

	cur, ok, err := st.LoadActionLogCursor(ctx, "p1")
	if err != nil || !ok {
		t.Fatalf("cursor: ok=%v err=%v", ok, err)
	}
	if cur.NextSinceID != 300 || !cur.CaughtUp {
		t.Errorf("cursor = %+v, want next=300 caught up", cur)
	}

	// A second pass resumes from the stored cursor rather than restarting.
	c2 := newActionLogClient(`{"entries":[],"has_more":false,"next_since_id":300}`)
	if _, err := CaptureActionLog(ctx, c2, st, "craftsman-1", now); err != nil {
		t.Fatal(err)
	}
	if len(c2.polls) != 1 || c2.polls[0] != 300 {
		t.Errorf("resumed polls = %v, want [300]", c2.polls)
	}
}

// TestCaptureActionLog_BudgetBoundsOnePass: a first capture of an 85-day log
// must not run until the server stops talking.
func TestCaptureActionLog_BudgetBoundsOnePass(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	replies := make([]string, actionLogPollsPerRun+5)
	for i := range replies {
		replies[i] = fullPage(int64(1000 + i*actionLogPageSize))
	}
	c := newActionLogClient(replies...)

	res, err := CaptureActionLog(ctx, c, st, "craftsman-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Polls != actionLogPollsPerRun {
		t.Errorf("polls = %d, want the budget %d", res.Polls, actionLogPollsPerRun)
	}
	if res.CaughtUp {
		t.Error("CaughtUp = true while pages were still full")
	}
	cur, _, _ := st.LoadActionLogCursor(ctx, "p1")
	if cur.CaughtUp {
		t.Error("cursor claims caught up mid-backfill")
	}
	if cur.NextSinceID == 0 {
		t.Error("cursor did not advance; the next pass would restart the walk")
	}
}

// TestCaptureActionLog_MissingNextSinceIDFallsBackToHighestID: without the
// fallback, a reply that omits next_since_id would make every future poll
// re-fetch the same window forever.
func TestCaptureActionLog_MissingNextSinceIDFallsBackToHighestID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	c := newActionLogClient(`{"entries":[
	  {"id":41,"event_type":"a.b","created_at":"2026-08-17T09:00:00Z"},
	  {"id":77,"event_type":"a.b","created_at":"2026-08-17T09:01:00Z"}],"has_more":false}`)

	if _, err := CaptureActionLog(ctx, c, st, "x", now); err != nil {
		t.Fatal(err)
	}
	cur, ok, err := st.LoadActionLogCursor(ctx, "p1")
	if err != nil || !ok {
		t.Fatalf("cursor: ok=%v err=%v", ok, err)
	}
	if cur.NextSinceID != 77 {
		t.Errorf("cursor = %d, want 77 (the highest id seen)", cur.NextSinceID)
	}
}

// TestCaptureActionLog_PollFailureKeepsProgress: a mid-walk failure must leave
// the cursor at the last confirmed position and must not fail the worker pass.
func TestCaptureActionLog_PollFailureKeepsProgress(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	c := newActionLogClient(fullPage(500))
	if _, err := CaptureActionLog(ctx, c, st, "x", now); err != nil {
		t.Fatal(err)
	}
	before, _, _ := st.LoadActionLogCursor(ctx, "p1")

	// Now every poll errors.
	failing := newActionLogClient()
	failing.err = errNoConnection
	res, err := CaptureActionLog(ctx, failing, st, "x", now)
	if err != nil {
		t.Fatalf("a failed poll must not fail the pass: %v", err)
	}
	if res.Polls != 0 || res.Inserted != 0 {
		t.Errorf("res = %+v, want nothing done", res)
	}
	after, _, _ := st.LoadActionLogCursor(ctx, "p1")
	if after.NextSinceID != before.NextSinceID {
		t.Errorf("cursor moved on a failed pass: %d -> %d", before.NextSinceID, after.NextSinceID)
	}
	if after.EventsStored != before.EventsStored {
		t.Errorf("events_stored moved on a failed pass: %d -> %d", before.EventsStored, after.EventsStored)
	}
}

// TestCaptureActionLog_PrunesInline: the pruning runs in the capture pass, not
// in a separate daemon nobody watches.
func TestCaptureActionLog_PrunesInline(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	if _, err := st.InsertActionLogEvents(ctx, "p1", []ActionLogEvent{
		{EventID: 1, EventType: "navigation.jumped", CreatedAt: "2026-08-01T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}
	c := newActionLogClient(`{"entries":[],"has_more":false,"next_since_id":1}`)
	res, err := CaptureActionLog(ctx, c, st, "x", now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Pruned != 1 {
		t.Errorf("pruned = %d, want 1", res.Pruned)
	}
}

// TestCaptureActionLog_NoStoreIsNotAnError keeps capture optional, the way a
// worker launched without --assets-db-path runs.
func TestCaptureActionLog_NoStoreIsNotAnError(t *testing.T) {
	if _, err := CaptureActionLog(context.Background(), newActionLogClient(), nil, "x", time.Now()); err != nil {
		t.Errorf("nil store: %v", err)
	}
}

var errNoConnection = fmt.Errorf("not connected")
