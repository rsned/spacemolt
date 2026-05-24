package main

import (
	"strings"
	"testing"
)

// liveQueryIntelPayload is the exact faction_query_intel OK payload observed
// from the server (trimmed to two systems for the test).
const liveQueryIntelPayload = `{"count":2,"entries":[{"connections":[{"distance":545,"system_id":"ross_128"},{"distance":573,"system_id":"pollux"}],"empire":"nebula","name":"Treasure Cache","pois":[{"class":"K7V","description":"A faint amber star.","id":"dim_fortune","name":"Dim Fortune","position":{"x":0,"y":0},"type":"sun"},{"class":"molecular_cloud","description":"Rich gas plume.","id":"treasure_cache_gas_plume","name":"Treasure Cache Gas Plume","position":{"x":4.7,"y":0.6},"resources":[{"remaining":28133,"resource_id":"hydrogen_gas","richness":47},{"remaining":9439,"resource_id":"plasma_gas","richness":26}],"type":"gas_cloud"}],"police_level":30,"submitted_at_tick":909252,"submitted_by":"86953","submitter_name":"Harrison 'Handiwork' Hay","system_id":"treasure_cache"},{"connections":[{"distance":374,"system_id":"moonshadow"}],"name":"Ross 128","pois":[{"description":"Prismatic nebulite.","id":"rainbow_nebulite_vein","name":"Rainbow Nebulite Vein","position":{"x":-1.3,"y":3.8},"resources":[{"remaining":5500,"resource_id":"prismatic_nebulite","richness":28}],"type":"gas_cloud"}],"police_level":0,"submitted_at_tick":909245,"submitted_by":"a509","submitter_name":"Arthur 'Artificer' Artis","system_id":"ross_128"}],"intel_level":1,"limit":50,"message":"Found 2 system(s) matching your query (showing 2).","total":2}`

func TestFormatFactionQueryIntel(t *testing.T) {
	out := formatFactionQueryIntel([]byte(liveQueryIntelPayload))
	if out == "" {
		t.Fatal("formatter returned empty string")
	}

	for _, want := range []string{
		"Found 2 system(s) matching your query (showing 2).",
		"Intel level: 1 | total: 2",
		"Treasure Cache (treasure_cache) — nebula, police 30",
		"by Harrison 'Handiwork' Hay @ tick 909252",
		"Dim Fortune (sun, K7V)",
		"Treasure Cache Gas Plume (gas_cloud, molecular_cloud)",
		"hydrogen_gas r47 28133",
		"Connections: ross_128 (545), pollux (573)",
		"Ross 128 (ross_128) — police 0", // no empire prefix when absent
		"prismatic_nebulite r28 5500",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestFormatFactionQueryIntel_Empty(t *testing.T) {
	if out := formatFactionQueryIntel([]byte(`nonsense`)); out != "" {
		t.Errorf("expected empty string for bad JSON, got %q", out)
	}
}
