package worker

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/customs"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// RefreshCustomsMap rebuilds the customs map from the captured action log,
// applies the operator overrides and writes it to mapPath. It backs both the
// hourly customs_map worker verb and the customs-map tool. A nil kb leaves
// empires that no inspection named blank.
func RefreshCustomsMap(ctx context.Context, db *sql.DB, kb knowledge.Base, mapPath, overridesPath string, now time.Time) (customs.Map, error) {
	o := customs.DefaultOptions()
	if kb != nil {
		o.EmpireOf = func(sys string) string {
			s, err := kb.GetSystem(ctx, sys)
			if err != nil || s == nil {
				return ""
			}
			return s.Empire
		}
	}
	stats, err := customs.Build(ctx, db, now, o)
	if err != nil {
		return customs.Map{}, err
	}
	ov, err := customs.LoadOverrides(overridesPath)
	if err != nil {
		return customs.Map{}, err
	}
	customs.Apply(stats, ov)
	m := customs.Map{GeneratedAt: now.UTC(), WindowDays: int(o.Window / (24 * time.Hour)), Systems: stats}
	if err := customs.Write(mapPath, m); err != nil {
		return customs.Map{}, err
	}
	return m, nil
}

// customsMapSummary is the one-line count the verb and tool print.
func customsMapSummary(m customs.Map) string {
	n := map[customs.Class]int{}
	for _, s := range m.Systems {
		n[s.Class]++
	}
	return fmt.Sprintf("customs_map: %d monitored, %d clear, %d unknown", n[customs.Monitored], n[customs.Clear], n[customs.Unknown])
}

// CustomsMapSummary exposes the summary line to cmd tools.
func CustomsMapSummary(m customs.Map) string { return customsMapSummary(m) }
