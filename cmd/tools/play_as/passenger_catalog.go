package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// cmdPassengerCatalog renders the stored galaxy-wide passenger catalog from the
// knowledge base. Usage:
//
//	passenger                 → list every stored passenger (Name (id), Empire, Bio)
//	passenger <citizen_id>    → full detail for one passenger
//	passenger --empire <name> → list only passengers from that empire
//
// It is a local KB read — no server round-trip — so it does not go through the
// styled-response formatters.
func cmdPassengerCatalog(ctx context.Context, args []string) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("passenger catalog requires a SQLite knowledge base")
	}

	// Parse args: a bare token is a citizen_id lookup; --empire <name> filters.
	var empire, lookupID string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--empire" || a == "-e":
			if i+1 < len(args) {
				i++
				empire = args[i]
			}
		case strings.HasPrefix(a, "--empire="):
			empire = strings.TrimPrefix(a, "--empire=")
		case lookupID == "":
			lookupID = a
		}
	}

	if lookupID != "" {
		return renderPassengerDetail(sqlite, lookupID)
	}
	return renderPassengerList(ctx, sqlite, empire)
}

// renderPassengerDetail prints the full record for a single passenger.
func renderPassengerDetail(sqlite *knowledge.SQLiteKB, citizenID string) error {
	p, err := sqlite.GetPassenger(citizenID)
	if err != nil {
		return fmt.Errorf("get passenger: %w", err)
	}
	if p == nil {
		fmt.Printf("  no passenger with citizen_id %q in the catalog\n", citizenID)
		return nil
	}
	fmt.Printf("%s (%s)\n", p.Name, p.CitizenID)
	fmt.Printf("  Empire: %s\n", orDash(p.Citizenship))
	fmt.Printf("  Class:  %s\n", orDash(p.Class))
	fmt.Printf("  Bio:    %s\n", orDash(p.Bio))
	if !p.SeenAt.IsZero() {
		fmt.Printf("  Seen:   %s\n", p.SeenAt.Format("2006-01-02 15:04"))
	}
	return nil
}

// renderPassengerList prints a one-line-per-passenger table of the catalog.
func renderPassengerList(ctx context.Context, sqlite *knowledge.SQLiteKB, empire string) error {
	pax, err := sqlite.ListPassengers(ctx, empire)
	if err != nil {
		return fmt.Errorf("list passengers: %w", err)
	}
	if len(pax) == 0 {
		if empire != "" {
			fmt.Printf("  (no passengers from %q in the catalog yet)\n", empire)
		} else {
			fmt.Println("  (no passengers in the catalog yet)")
		}
		return nil
	}

	// Column widths: "Name (id)" and empire, sized to content.
	nameW, empW := 0, len("Empire")
	whos := make([]string, len(pax))
	for i, p := range pax {
		whos[i] = fmt.Sprintf("%s (%s)", p.Name, p.CitizenID)
		nameW = max(nameW, len(whos[i]))
		empW = max(empW, len(p.Citizenship))
	}

	fmt.Printf("  %-*s  %-*s  %s\n", nameW, "Name (id)", empW, "Empire", "Bio")
	fmt.Printf("  %s  %s  %s\n", strings.Repeat("-", nameW), strings.Repeat("-", empW), strings.Repeat("-", 40))
	for i, p := range pax {
		fmt.Printf("  %-*s  %-*s  %s\n", nameW, whos[i], empW, orDash(p.Citizenship), truncatePax(p.Bio, 60))
	}
	fmt.Printf("  %d passenger(s) in the catalog\n", len(pax))
	return nil
}

// orDash returns s, or "-" when s is empty, for tidy column rendering.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// truncatePax shortens s to n runes, appending an ellipsis when it was cut.
func truncatePax(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
