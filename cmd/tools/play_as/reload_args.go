package main

import (
	"fmt"
	"strings"
)

// reloadArgs resolves `reload`'s two ids from either the positional form
//
//	reload <weapon-instance-id> <ammo-item-id>
//
// or the flag form
//
//	reload --weapon_instance_id=<id> --ammo_item_id=<id>
//
// mirroring what `scan` already does for its target. reload accepted only the
// positional form, so a flag invocation was forwarded VERBATIM as the ids and
// came back a tick later as unknown_weapon with "--weapon_instance_id=..."
// quoted as the id. That is the benign outcome; a flag token that happened to
// match a real id would not be.
//
// --ammo_id is accepted as an alias for --ammo_item_id. It is the obvious guess
// (the weapon flag is named for its instance, the ammo one for its item), and
// refusing it would be an error about a distinction with no meaning.
//
// parts is the whole command line, so parts[0] is "reload".
func reloadArgs(parts []string) (weaponInstanceID, ammoItemID string, err error) {
	const usage = "usage: reload <weapon-instance-id> <ammo-item-id>\n" +
		"   or: reload --weapon_instance_id=<id> --ammo_item_id=<id>"

	args := parts[1:]
	flagged := false
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			flagged = true

			break
		}
	}

	if !flagged {
		if len(args) < 2 {
			return "", "", fmt.Errorf("%s", usage)
		}

		return args[0], strings.ToLower(args[1]), nil
	}

	// parseFlagArgs silently DROPS keys it was not told about — sensible for
	// commands that parse a subset, wrong here: a misspelled flag would vanish
	// and surface as the generic usage error, which is the same unhelpful
	// answer the positional-only path gave. Name it instead.
	known := map[string]bool{"weapon_instance_id": true, "ammo_item_id": true, "ammo_id": true}
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			continue
		}
		key, _, _ := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !known[key] {
			return "", "", fmt.Errorf("unknown flag --%s\n%s", key, usage)
		}
	}

	flags, err := parseFlagArgs(args, "weapon_instance_id", "ammo_item_id", "ammo_id")
	if err != nil {
		return "", "", err
	}

	weaponInstanceID, _ = flags["weapon_instance_id"].(string)
	ammoItemID, _ = flags["ammo_item_id"].(string)
	if ammoItemID == "" {
		ammoItemID, _ = flags["ammo_id"].(string)
	}
	if weaponInstanceID == "" || ammoItemID == "" {
		return "", "", fmt.Errorf("%s", usage)
	}

	return weaponInstanceID, strings.ToLower(ammoItemID), nil
}
