package mbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Blocklist is a persisted set of spam senders for a single agent. Entries are
// stored verbatim (preserving display case) but matched case-insensitively
// against either a message's sender_id or sender display name. It is backed by
// a JSON array of strings on disk, e.g. data/agents/<agent>/spam_list.json.
type Blocklist struct {
	path    string
	mu      sync.RWMutex
	entries map[string]string // lowercased key -> original entry
}

// LoadBlocklist reads the blocklist at path. A missing file yields an empty
// blocklist with no error so first-time callers don't have to special-case it.
func LoadBlocklist(path string) (*Blocklist, error) {
	bl := &Blocklist{path: path, entries: make(map[string]string)}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return bl, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mbox: read blocklist: %w", err)
	}

	var arr []string
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, fmt.Errorf("mbox: parse blocklist %q: %w", path, err)
		}
	}
	for _, e := range arr {
		bl.entries[strings.ToLower(e)] = e
	}
	return bl, nil
}

// IsBlocked reports whether a message from senderID/sender should be treated as
// spam. A match on either field (case-insensitive) blocks the message.
func (b *Blocklist) IsBlocked(senderID, sender string) bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if senderID != "" {
		if _, ok := b.entries[strings.ToLower(senderID)]; ok {
			return true
		}
	}
	if sender != "" {
		if _, ok := b.entries[strings.ToLower(sender)]; ok {
			return true
		}
	}
	return false
}

// Add inserts entry into the blocklist and persists it. It returns (true, nil)
// if the entry was newly added, or (false, nil) if it was already present
// (case-insensitive).
func (b *Blocklist) Add(entry string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := strings.ToLower(entry)
	if _, ok := b.entries[key]; ok {
		return false, nil
	}
	b.entries[key] = entry
	if err := b.save(); err != nil {
		delete(b.entries, key)
		return false, err
	}
	return true, nil
}

// Remove deletes entry from the blocklist and persists the change. It returns
// (true, nil) if the entry was present and removed, or (false, nil) if it was
// not in the list (case-insensitive).
func (b *Blocklist) Remove(entry string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := strings.ToLower(entry)
	prev, ok := b.entries[key]
	if !ok {
		return false, nil
	}
	delete(b.entries, key)
	if err := b.save(); err != nil {
		b.entries[key] = prev
		return false, err
	}
	return true, nil
}

// List returns the blocked entries in their original case, sorted.
func (b *Blocklist) List() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.entries))
	for _, v := range b.entries {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// save writes the current entries to disk as a JSON array. Callers must hold
// the write lock.
func (b *Blocklist) save() error {
	out := make([]string, 0, len(b.entries))
	for _, v := range b.entries {
		out = append(out, v)
	}
	sort.Strings(out)

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("mbox: marshal blocklist: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return fmt.Errorf("mbox: create blocklist dir: %w", err)
	}
	// Atomic write via temp file + rename.
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("mbox: write blocklist: %w", err)
	}
	if err := os.Rename(tmp, b.path); err != nil {
		return fmt.Errorf("mbox: replace blocklist: %w", err)
	}
	return nil
}
