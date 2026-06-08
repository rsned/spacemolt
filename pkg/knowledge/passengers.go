package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SeenPassenger is a single passenger (citizen) observation. Mirrors the shape
// of game.ObservedPassenger but lives in pkg/knowledge so this package does not
// import pkg/game. Callers adapt one to the other.
//
// The store is identity-only: it captures who a passenger is (name, empire,
// bio, preferred travel class), not where they were going or what they paid.
type SeenPassenger struct {
	CitizenID   string
	Name        string
	Citizenship string // the passenger's empire of origin
	Bio         string
	Class       string // travel class preference (economy | business | first)

	Source string // "list_station_passengers" | "list_passengers" | "dock" | ...
	SeenAt time.Time
}

// RecordPassengers inserts/updates rows in the passengers catalog for each
// observation. All writes share a single transaction. Records with an empty
// CitizenID are silently dropped. Empire/bio/class are COALESCE-merged so a
// sparse source (e.g. an aboard manifest lacking citizenship) never wipes
// richer data already captured from a station listing.
func (kb *SQLiteKB) RecordPassengers(obs []SeenPassenger) error {
	if len(obs) == 0 {
		return nil
	}

	tx, err := kb.db.Begin()
	if err != nil {
		return fmt.Errorf("knowledge: begin passengers tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, o := range obs {
		if o.CitizenID == "" {
			continue
		}
		seenStr := o.SeenAt.UTC().Format(time.RFC3339)

		if _, err := tx.Exec(`
INSERT INTO passengers
	(citizen_id, name, citizenship, bio, class,
	 first_seen_utc, last_seen_utc, sighting_count)
VALUES (?, ?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(citizen_id) DO UPDATE SET
	name           = excluded.name,
	citizenship    = COALESCE(NULLIF(excluded.citizenship, ''), citizenship),
	bio            = COALESCE(NULLIF(excluded.bio, ''), bio),
	class          = COALESCE(NULLIF(excluded.class, ''), class),
	last_seen_utc  = excluded.last_seen_utc,
	sighting_count = sighting_count + 1`,
			o.CitizenID, o.Name, o.Citizenship, o.Bio, o.Class,
			seenStr, seenStr,
		); err != nil {
			return fmt.Errorf("knowledge: upsert passengers: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit passengers: %w", err)
	}
	return nil
}

// GetPassenger returns the stored catalog row for a citizen_id, or (nil, nil)
// if no such row exists.
func (kb *SQLiteKB) GetPassenger(citizenID string) (*SeenPassenger, error) {
	if citizenID == "" {
		return nil, nil
	}
	var (
		out  SeenPassenger
		cit  sql.NullString
		bio  sql.NullString
		cls  sql.NullString
		last string
	)
	err := kb.db.QueryRow(`
SELECT citizen_id, name, citizenship, bio, class, last_seen_utc
FROM passengers WHERE citizen_id = ?`, citizenID,
	).Scan(&out.CitizenID, &out.Name, &cit, &bio, &cls, &last)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("knowledge: get passenger %s: %w", citizenID, err)
	}
	out.Citizenship = cit.String
	out.Bio = bio.String
	out.Class = cls.String
	if t, perr := time.Parse(time.RFC3339, last); perr == nil {
		out.SeenAt = t
	}
	return &out, nil
}

// ListPassengers returns every stored passenger, ordered by name. When
// citizenship is non-empty it filters to that empire (case-sensitive match on
// the stored value).
func (kb *SQLiteKB) ListPassengers(ctx context.Context, citizenship string) ([]SeenPassenger, error) {
	query := `
SELECT citizen_id, name, citizenship, bio, class, last_seen_utc
FROM passengers`
	var (
		rows *sql.Rows
		err  error
	)
	if citizenship != "" {
		query += " WHERE citizenship = ? ORDER BY name"
		rows, err = kb.db.QueryContext(ctx, query, citizenship)
	} else {
		query += " ORDER BY name"
		rows, err = kb.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("knowledge: list passengers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SeenPassenger
	for rows.Next() {
		var (
			p    SeenPassenger
			cit  sql.NullString
			bio  sql.NullString
			cls  sql.NullString
			last string
		)
		if err := rows.Scan(&p.CitizenID, &p.Name, &cit, &bio, &cls, &last); err != nil {
			return nil, fmt.Errorf("knowledge: scan passenger: %w", err)
		}
		p.Citizenship = cit.String
		p.Bio = bio.String
		p.Class = cls.String
		if t, perr := time.Parse(time.RFC3339, last); perr == nil {
			p.SeenAt = t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
