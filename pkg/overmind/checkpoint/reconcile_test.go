package checkpoint

import "testing"

func TestReconcileResumeWhenMatching(t *testing.T) {
	saved := KnownState{System: "SOL", POI: "ST-9", Docked: true, Credits: 1000}
	live := saved
	r := Reconcile(saved, live, 0.25)
	if r.Disposition != Resume || len(r.Reasons) != 0 {
		t.Fatalf("expected clean Resume, got %+v", r)
	}
}

func TestReconcileDivergedOnSystemChange(t *testing.T) {
	saved := KnownState{System: "SOL", Docked: true, Credits: 1000}
	live := KnownState{System: "VEGA", Docked: true, Credits: 1000}
	r := Reconcile(saved, live, 0.25)
	if r.Disposition != Diverged || len(r.Reasons) == 0 {
		t.Fatalf("expected Diverged with reason, got %+v", r)
	}
}

func TestReconcileDivergedOnCreditDrop(t *testing.T) {
	saved := KnownState{System: "SOL", Credits: 1000}
	live := KnownState{System: "SOL", Credits: 500} // 50% drop > 25%
	r := Reconcile(saved, live, 0.25)
	if r.Disposition != Diverged {
		t.Fatalf("expected Diverged on credit drop, got %+v", r)
	}
}

func TestReconcileFreshWorker(t *testing.T) {
	r := Reconcile(KnownState{}, KnownState{System: "SOL"}, 0.25)
	if r.Disposition != Resume {
		t.Fatalf("fresh worker should Resume, got %+v", r)
	}
}
