package knowledge

import "strings"

// ProtectedStageWords are the growth-stage words a creature name can carry.
//
// They serve two purposes at once. They mark a PROTECTED juvenile, which must
// not be hunted; and they are why wildlife_species.name needed canonicalising,
// since the table holds one name per species and a stage-qualified name would
// otherwise overwrite the mature one and flip back on the next sighting.
//
// Supplied by the operator on 2026-10-14 after "Juvenile Hoarfrost Leviathan"
// turned up at zibal_ice_shelf alongside the "Young Cloudwhale" at
// yarrowfield_vapor_fields. Both are leviathan-class, which is also the only
// class whose hull varies per individual -- every one of the other 25 species
// observed has a single fixed hull -- so a stage word and a growing hull are
// two views of the same mechanic.
//
// A stage can lead the name ("Young Cloudwhale") or trail it ("Cloudwhale
// Calf"), so both ends are checked.
var ProtectedStageWords = []string{"hatchling", "young", "calf", "juvenile"}

// IsProtectedJuvenile reports whether a creature display name marks a protected
// juvenile that must not be hunted.
//
// Matching is case-insensitive and on WHOLE words only: "Youngling Thing" and
// "Calfax Drifter" are not juveniles, and a substring match would both spare
// adults by accident and mangle names in CanonicalSpeciesName.
func IsProtectedJuvenile(name string) bool {
	return StageWord(name) != ""
}

// StageWord returns the growth-stage word in a creature display name,
// lowercased, or "" when there is none.
//
// "" is a VALUE, not a failure: it is the mature bucket. A fully grown
// "Cloudwhale" carries no stage word, which is exactly what distinguishes it
// from a "Young Cloudwhale", so an observation row stores "" for an adult
// rather than leaving the stage unknown.
//
// This is the half CanonicalSpeciesName discards. Observation rows keep both:
// the stage as its own column so a per-stage histogram needs no name parsing,
// and the raw name beside it so history can be recomputed if the vocabulary
// ever gains a word.
func StageWord(name string) string {
	_, stage := splitStage(name)

	return stage
}

// CanonicalSpeciesName returns the display name with any growth-stage word
// removed, so every stage of one species collapses to a single stable name.
//
// "Young Cloudwhale" and a mature "Cloudwhale" both yield "Cloudwhale", which
// is what wildlife_species.name should hold. The per-individual stage is not
// lost: it stays on the observation rows and on wildlife_kills.creature_name,
// which is where a per-stage histogram reads it from.
func CanonicalSpeciesName(name string) string {
	stripped, stage := splitStage(name)
	if stage == "" {
		return name
	}

	return stripped
}

// splitStage removes a leading or trailing stage word, returning the remaining
// name and the stage in its normalised form ("" when there is none).
//
// A name consisting ONLY of a stage word is left alone: there is no species
// left to call it by, so the original is more useful than "", and reporting a
// stage for it would file every such sighting under a species of one word.
func splitStage(name string) (string, string) {
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return name, ""
	}
	if s := stageWordOf(fields[0]); s != "" {
		return strings.Join(fields[1:], " "), s
	}
	if s := stageWordOf(fields[len(fields)-1]); s != "" {
		return strings.Join(fields[:len(fields)-1], " "), s
	}

	return name, ""
}

// stageWordOf normalises one field and returns it when it is a stage word,
// otherwise "". Matching is on the WHOLE field: a substring match would both
// spare adults by accident ("Youngling") and mangle names in
// CanonicalSpeciesName.
func stageWordOf(w string) string {
	w = strings.ToLower(strings.Trim(w, ".,-—"))
	for _, s := range ProtectedStageWords {
		if w == s {
			return s
		}
	}

	return ""
}
