package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Registry holds loaded skill definitions indexed by name.
type Registry struct {
	skills map[string]*Skill
}

// LoadRegistry reads all .yaml and .yml files from a directory into a registry.
func LoadRegistry(dir string) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading skills directory: %w", err)
	}

	reg := &Registry{skills: make(map[string]*Skill)}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		skill, loadErr := LoadSkill(path)
		if loadErr != nil {
			return nil, fmt.Errorf("loading %s: %w", entry.Name(), loadErr)
		}

		if _, exists := reg.skills[skill.Name]; exists {
			return nil, fmt.Errorf("duplicate skill name %q in %s", skill.Name, entry.Name())
		}
		reg.skills[skill.Name] = skill
	}

	return reg, nil
}

// Get returns a skill by name, or nil if not found.
func (r *Registry) Get(name string) *Skill {
	return r.skills[name]
}

// Has returns true if a skill with the given name exists.
func (r *Registry) Has(name string) bool {
	_, ok := r.skills[name]
	return ok
}

// Names returns all registered skill names sorted alphabetically.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
