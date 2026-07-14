package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config represents the maia configuration
type Config struct {
	// LLM settings
	OpenAIAPIKey  string `json:"openai_api_key"`
	OpenAIBaseURL string `json:"openai_base_url"`
	Model         string `json:"model"`

	// Web search
	BraveAPIKey string `json:"brave_api_key"`

	// Custom settings
	Custom map[string]string `json:"custom,omitempty"`
}

var (
	globalConfig *Config
	configPath   string
)

func init() {
	home, _ := os.UserHomeDir()
	configPath = filepath.Join(home, ".maia", "config.json")
}

// Load loads the config from ~/.maia/config.json
func Load() (*Config, error) {
	if globalConfig != nil {
		return globalConfig, nil
	}

	globalConfig = &Config{}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return globalConfig, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	if err := json.Unmarshal(data, globalConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return globalConfig, nil
}

// Get retrieves a config value, resolving commands if needed
func Get(key string) string {
	config, _ := Load()

	// Check environment variables first (they override config)
	if envVal := os.Getenv(envName(key)); envVal != "" {
		return envVal
	}

	// Get value from config
	var value string
	switch key {
	case "openai_api_key":
		value = config.OpenAIAPIKey
	case "openai_base_url":
		value = config.OpenAIBaseURL
	case "model":
		value = config.Model
	case "brave_api_key":
		value = config.BraveAPIKey
	default:
		if config.Custom != nil {
			value = config.Custom[key]
		}
	}

	// Resolve command if needed
	return resolveCommand(value)
}

// resolveCommand handles {cmd:...} syntax
func resolveCommand(value string) string {
	if !strings.HasPrefix(value, "{cmd:") || !strings.HasSuffix(value, "}") {
		return value
	}

	// Extract command
	cmdStr := strings.TrimPrefix(value, "{cmd:")
	cmdStr = strings.TrimSuffix(cmdStr, "}")

	// Parse command with quote support
	parts := parseCommand(cmdStr)
	if len(parts) == 0 {
		return ""
	}

	// Execute command
	cmd := exec.Command(parts[0], parts[1:]...)
	output, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to execute command '%s': %v\n", cmdStr, err)
		return ""
	}

	return strings.TrimSpace(string(output))
}

// parseCommand splits a command string respecting quoted arguments
func parseCommand(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]

		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
		} else {
			// Single quote (39) or double quote (34)
			if c == 39 || c == 34 {
				inQuote = true
				quoteChar = c
			} else if c == ' ' || c == '\t' {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				current.WriteByte(c)
			}
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func envName(key string) string {
	// Handle known mappings
	switch key {
	case "openai_api_key":
		return "OPENAI_API_KEY"
	case "openai_base_url":
		return "OPENAI_BASE_URL"
	case "model":
		return "MAIA_MODEL"
	case "brave_api_key":
		return "BRAVE_API_KEY"
	}

	// Convert snake_case to UPPER_SNAKE_CASE with MAIA_ prefix
	env := "MAIA_" + strings.ToUpper(strings.ReplaceAll(key, "_", "_"))
	return env
}

// Save saves the config to ~/.maia/config.json
func Save(config *Config) error {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600)
}

// ConfigPath returns the path to the config file
func ConfigPath() string {
	return configPath
}
