package unified

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the unified server configuration.
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Game        GameConfig        `yaml:"game"`
	Agents      AgentsConfig      `yaml:"agents"`
	LLM         LLMConfig         `yaml:"llm"`
	Database    DatabaseConfig    `yaml:"database"`
	Credentials CredentialsConfig `yaml:"credentials"`
	Strategies  StrategiesConfig  `yaml:"strategies"`
	Teams       []TeamConfig      `yaml:"teams"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	HTTPPort      int    `yaml:"http_port"`
	StaticDir     string `yaml:"static_dir"`
	AdminStaticDir string `yaml:"admin_static_dir"`
}

// GameConfig holds game server connection settings.
type GameConfig struct {
	ServerURL string `yaml:"server_url"`
}

// AgentsConfig holds agent management settings.
type AgentsConfig struct {
	Dir              string        `yaml:"dir"`
	Max              int           `yaml:"max"`
	DecisionInterval time.Duration `yaml:"decision_interval"`
	Enabled          []string      `yaml:"enabled"`
}

// LLMConfig holds LLM provider settings.
type LLMConfig struct {
	URL     string        `yaml:"url"`
	Model   string        `yaml:"model"`
	Timeout time.Duration `yaml:"timeout"`
}

// DatabaseConfig holds knowledge base settings.
type DatabaseConfig struct {
	Backend string `yaml:"backend"`
	Path    string `yaml:"path"`
}

// CredentialsConfig holds credential storage settings.
type CredentialsConfig struct {
	Backend string `yaml:"backend"`
	Path    string `yaml:"path"`
}

// StrategiesConfig holds strategy assignment defaults.
type StrategiesConfig struct {
	Defaults map[string]string `yaml:"defaults"`
}

// TeamConfig defines a team in the configuration.
type TeamConfig struct {
	Name    string             `yaml:"name"`
	Leader  string             `yaml:"leader"`
	Members []TeamMemberConfig `yaml:"members"`
}

// TeamMemberConfig defines a team member in the configuration.
type TeamMemberConfig struct {
	AgentID string `yaml:"agent_id"`
	Role    string `yaml:"role"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			HTTPPort:  8090,
			StaticDir: "frontend/dist",
		},
		Game: GameConfig{
			ServerURL: "wss://game.spacemolt.com/ws",
		},
		Agents: AgentsConfig{
			Dir:              "data/agents",
			Max:              50,
			DecisionInterval: 11 * time.Second,
		},
		LLM: LLMConfig{
			URL:     "http://localhost:11434",
			Model:   "llama3.2",
			Timeout: 60 * time.Second,
		},
		Database: DatabaseConfig{
			Backend: "sqlite",
			Path:    "data/spacemolt-knowledge.db",
		},
		Credentials: CredentialsConfig{
			Backend: "file",
			Path:    "data/credentials",
		},
		Strategies: StrategiesConfig{
			Defaults: make(map[string]string),
		},
	}
}

// LoadConfig reads a YAML config file and returns the parsed Config.
// Missing fields are filled with defaults.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}
