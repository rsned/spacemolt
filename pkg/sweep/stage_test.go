package sweep

import "testing"

func TestParseStage(t *testing.T) {
	st, err := ParseStage("node_beta, gsc_0049 ,ash_pearl")
	if err != nil {
		t.Fatal(err)
	}
	if st != (Stage{Refuel: "node_beta", Launch: "gsc_0049", Target: "ash_pearl"}) {
		t.Errorf("stage = %+v", st)
	}
	if st, err := ParseStage(""); err != nil || st.Set() {
		t.Errorf("empty: %+v %v, want unset, no error", st, err)
	}
	for _, bad := range []string{"node_beta,gsc_0049", "a,,c", "a,b,c,d"} {
		if _, err := ParseStage(bad); err == nil {
			t.Errorf("ParseStage(%q) accepted", bad)
		}
	}
}
