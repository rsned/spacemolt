package main

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// StatuslineConfig holds all statusline configuration.
type StatuslineConfig struct {
	Enabled        bool              `yaml:"enabled"`
	Format         string            `yaml:"format"`
	BarStyle       string            `yaml:"bar_style"`
	BarWidth       int               `yaml:"bar_width"`
	Thresholds     ThresholdConfig   `yaml:"thresholds"`
	SecurityColors map[string]string `yaml:"security_colors"`
	Indicators     IndicatorConfig   `yaml:"indicators"`
}

// ThresholdConfig defines percentage thresholds for color coding.
type ThresholdConfig struct {
	Critical int `yaml:"critical"`
	Warning  int `yaml:"warning"`
	Good     int `yaml:"good"`
}

// IndicatorConfig defines text indicators for various states.
type IndicatorConfig struct {
	Docked       string `yaml:"docked"`
	InSpace      string `yaml:"in_space"`
	InCombat     string `yaml:"in_combat"`
	Cloaked      string `yaml:"cloaked"`
	Reconnecting string `yaml:"reconnecting"`
}

// PlayAsConfig is the top-level config file structure.
type PlayAsConfig struct {
	OutputFormat string           `yaml:"output_format"` // "raw", "json", or "styled"
	Statusline   StatuslineConfig `yaml:"statusline"`
}

func defaultConfig() PlayAsConfig {
	return PlayAsConfig{
		OutputFormat: "json",
		Statusline: StatuslineConfig{
			Enabled:  true,
			Format:   "{agent} {conn} | {system} {poi} {dock} | {ship_class} | Hull:{hull} Shld:{shield} Fuel:{fuel} Cargo:{cargo} | Tick:{tick}",
			BarStyle: "bar",
			BarWidth: 6,
			Thresholds: ThresholdConfig{
				Critical: 15,
				Warning:  35,
				Good:     65,
			},
			SecurityColors: map[string]string{
				"High Security":    "green",
				"Medium Security":  "yellow",
				"Low Security":     "red",
				"Lawless":          "bright_red",
				"Pirate Stronghold": "magenta",
			},
			Indicators: IndicatorConfig{
				Docked:       "⚓Docked",
				InSpace:      "🚀Space",
				InCombat:     "⚔COMBAT",
				Cloaked:      "👻Cloak",
				Reconnecting: "⟳reconnecting",
			},
		},
	}
}

// defaultConfigPath returns ~/.config/spacemolt/play_as.yaml.
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "spacemolt", "play_as.yaml")
}

// loadConfig loads the config from the given path, falling back to defaults
// for any missing fields. Returns defaults if the file doesn't exist.
func loadConfig(path string) PlayAsConfig {
	cfg := defaultConfig()

	if path == "" {
		return cfg
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	var fileCfg PlayAsConfig
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return cfg
	}

	// Merge: only override non-zero values from file.
	if fileCfg.OutputFormat != "" {
		cfg.OutputFormat = fileCfg.OutputFormat
	}

	sl := fileCfg.Statusline

	// enabled is tricky since false is zero value — if the file was parsed
	// and has a statusline section at all, respect its enabled field.
	// We detect this by checking if any statusline field was set.
	if sl.Format != "" || sl.BarStyle != "" || sl.BarWidth != 0 || !sl.Enabled {
		cfg.Statusline.Enabled = sl.Enabled
	}

	if sl.Format != "" {
		cfg.Statusline.Format = sl.Format
	}
	if sl.BarStyle != "" {
		cfg.Statusline.BarStyle = sl.BarStyle
	}
	if sl.BarWidth > 0 {
		cfg.Statusline.BarWidth = sl.BarWidth
	}
	if sl.Thresholds.Critical > 0 {
		cfg.Statusline.Thresholds.Critical = sl.Thresholds.Critical
	}
	if sl.Thresholds.Warning > 0 {
		cfg.Statusline.Thresholds.Warning = sl.Thresholds.Warning
	}
	if sl.Thresholds.Good > 0 {
		cfg.Statusline.Thresholds.Good = sl.Thresholds.Good
	}
	if len(sl.SecurityColors) > 0 {
		cfg.Statusline.SecurityColors = sl.SecurityColors
	}
	if sl.Indicators.Docked != "" {
		cfg.Statusline.Indicators.Docked = sl.Indicators.Docked
	}
	if sl.Indicators.InSpace != "" {
		cfg.Statusline.Indicators.InSpace = sl.Indicators.InSpace
	}
	if sl.Indicators.InCombat != "" {
		cfg.Statusline.Indicators.InCombat = sl.Indicators.InCombat
	}
	if sl.Indicators.Cloaked != "" {
		cfg.Statusline.Indicators.Cloaked = sl.Indicators.Cloaked
	}
	if sl.Indicators.Reconnecting != "" {
		cfg.Statusline.Indicators.Reconnecting = sl.Indicators.Reconnecting
	}

	return cfg
}
