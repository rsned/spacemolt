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
	_, found := splitStage(name)

	return found
}

// CanonicalSpeciesName returns the display name with any growth-stage word
// removed, so every stage of one species collapses to a single stable name.
//
// "Young Cloudwhale" and a mature "Cloudwhale" both yield "Cloudwhale", which
// is what wildlife_species.name should hold. The per-individual stage is not
// lost: it stays on the observation rows and on wildlife_kills.creature_name,
// which is where a per-stage histogram reads it from.
func CanonicalSpeciesName(name string) string {
	stripped, found := splitStage(name)
	if !found {
		return name
	}

	return stripped
}

// splitStage removes a leading or trailing stage word, reporting whether one
// was present. A name consisting ONLY of a stage word is left alone: there is
// no species left to call it by, so the original is more useful than "".
func splitStage(name string) (string, bool) {
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return name, false
	}
	if isStageWord(fields[0]) {
		return strings.Join(fields[1:], " "), true
	}
	if isStageWord(fields[len(fields)-1]) {
		return strings.Join(fields[:len(fields)-1], " "), true
	}

	return name, false
}

func isStageWord(w string) bool {
	w = strings.ToLower(strings.Trim(w, ".,-—"))
	for _, s := range ProtectedStageWords {
		if w == s {
			return true
		}
	}

	return false
}
