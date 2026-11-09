package main

// stationModel is one hologram model offered by the console.
type stationModel struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

// stationModels are the nine spec models; the first is the default. The id
// is the only thing a request may name: files are located by looking the id
// up here, never by joining request text onto a path.
var stationModels = []stationModel{
	{"player__wheel__capital_s5521", "Player wheel (capital)", true},
	{"nebula__drum__moderate_s5230", "Nebula drum (moderate)", false},
	{"solarian__spindle__moderate_s5060", "Solarian spindle (moderate)", false},
	{"crimson__ring__moderate_s5310", "Crimson ring (moderate)", false},
	{"crimson__wheel__moderate_s5320", "Crimson wheel (moderate)", false},
	{"voidborn__pylon_toroid__moderate_s5440", "Voidborn pylon toroid (moderate)", false},
	{"voidborn__spindle__capital_s5461", "Voidborn spindle (capital)", false},
	{"player__drum__capital_s5531", "Player drum (capital)", false},
	{"player__terraces__capital_s5551", "Player terraces (capital)", false},
}

func findModel(id string) (stationModel, bool) {
	for _, m := range stationModels {
		if m.ID == id {
			return m, true
		}
	}
	return stationModel{}, false
}
