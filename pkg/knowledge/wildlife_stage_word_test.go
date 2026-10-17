package knowledge

import "testing"

// TestStageWord covers extracting the growth stage itself, which
// CanonicalSpeciesName throws away. Observation rows store the stage as its own
// column so a per-stage histogram does not re-parse display names on every
// query.
func TestStageWord(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		// Attested in the wild.
		{"Hoarfrost Leviathan Hatchling", "hatchling"}, // trailing
		{"Juvenile Molt Leviathan", "juvenile"},        // leading
		{"Young Cloudwhale", "young"},
		{"Cloudwhale Calf", "calf"},

		// Mature: no stage word, and that is a value, not a failure -- "" is
		// the adult bucket.
		{"Cloudwhale", ""},
		{"Rainbow Leviathan", ""},
		{"Belt-Grazer", ""},

		// Case and punctuation are normalised, matching stageWordOf.
		{"JUVENILE Molt Leviathan", "juvenile"},
		{"Cloudwhale, calf", "calf"},

		// Whole-word only: a substring must not register.
		{"Youngling Thing", ""},
		{"Calfax Drifter", ""},

		// A bare stage word has no species left, so it is not split.
		{"Hatchling", ""},
		{"", ""},
	} {
		if got := StageWord(tc.name); got != tc.want {
			t.Errorf("StageWord(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestStageWordAgreesWithCanonical pins the two functions to one parse: a name
// has a stage word if and only if canonicalising it changes the name.
func TestStageWordAgreesWithCanonical(t *testing.T) {
	for _, n := range []string{
		"Hoarfrost Leviathan Hatchling", "Juvenile Molt Leviathan", "Young Cloudwhale",
		"Cloudwhale Calf", "Cloudwhale", "Belt-Grazer", "Youngling Thing", "Hatchling", "",
	} {
		hasStage := StageWord(n) != ""
		changed := CanonicalSpeciesName(n) != n
		if hasStage != changed {
			t.Errorf("%q: StageWord gives %q but CanonicalSpeciesName %s the name",
				n, StageWord(n), map[bool]string{true: "changed", false: "kept"}[changed])
		}
		if hasStage != IsProtectedJuvenile(n) {
			t.Errorf("%q: StageWord=%q disagrees with IsProtectedJuvenile=%v",
				n, StageWord(n), IsProtectedJuvenile(n))
		}
	}
}
