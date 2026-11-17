package main

// consoleStation is one of our stations (or outposts) the console can show.
type consoleStation struct {
	ID      string `json:"id"`     // base id: the key for base_snapshots
	Name    string `json:"name"`   // display name
	System  string `json:"system"` // display system name
	Model   string `json:"model"`  // default hologram model id (a stationModels id)
	Default bool   `json:"default"`
}

// consoleStations are the stations the console offers, in rail order; the
// first is the default. Like stationModels, the id is the only thing a
// request may name: a ?station= value is looked up here and refused if absent.
var consoleStations = []consoleStation{
	{ID: "c976fa7df53c87f9616165d7d411b41a", Name: "Conjunction Junction", System: "Cloverfield", Model: "player__wheel__capital_s5521", Default: true},
	{ID: "b7ecd9b1ce7c2689d73d4f15f30dd134", Name: "Jim Bob's Backwater Shack", System: "Beid", Model: "player__drum__capital_s5531"},
}

func findStation(id string) (consoleStation, bool) {
	for _, st := range consoleStations {
		if st.ID == id {
			return st, true
		}
	}
	return consoleStation{}, false
}
