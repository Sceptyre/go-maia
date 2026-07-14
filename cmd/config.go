package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sceptyre/maia/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show maia configuration",
	Long:  `Display the current configuration and where values are sourced from.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("Maia Configuration")
		fmt.Println(strings.Repeat("─", 50))
		fmt.Printf("Config file: %s\n\n", config.ConfigPath())

		// Check if config file exists
		if _, err := os.Stat(config.ConfigPath()); os.IsNotExist(err) {
			fmt.Println("⚠ Config file not found")
			fmt.Println("\nCreate ~/.maia/config.json with:")
			fmt.Println(`{
  "openai_api_key": "your-key"
}`)
			fmt.Println("\nOr use command syntax for secrets:")
			fmt.Println(`{
  "openai_api_key": "{cmd:op read op://vault/mcp-api/token}"
}`)
			return nil
		}

		fmt.Println("Settings:")
		fmt.Println()

		// OpenAI API Key
		apiKey := config.Get("openai_api_key")
		if apiKey != "" {
			masked := apiKey[:min(8, len(apiKey))] + "..."
			fmt.Printf("  openai_api_key: %s\n", masked)
			if os.Getenv("OPENAI_API_KEY") != "" {
				fmt.Printf("    Source: OPENAI_API_KEY env var\n")
			} else if cfg.OpenAIAPIKey != "" {
				if strings.HasPrefix(cfg.OpenAIAPIKey, "{cmd:") {
					fmt.Printf("    Source: config.json (command)\n")
				} else {
					fmt.Printf("    Source: config.json\n")
				}
			}
		} else {
			fmt.Printf("  openai_api_key: (not set)\n")
		}

		// Base URL
		baseURL := config.Get("openai_base_url")
		if baseURL != "" {
			fmt.Printf("  openai_base_url: %s\n", baseURL)
		}

		// Model
		model := config.Get("model")
		if model != "" {
			fmt.Printf("  model: %s\n", model)
		}

		// Brave API Key
		braveKey := config.Get("brave_api_key")
		if braveKey != "" {
			fmt.Printf("  brave_api_key: set\n")
		} else {
			fmt.Printf("  brave_api_key: (not set) - web search limited\n")
		}

		fmt.Println()
		fmt.Println("Environment variables override config file values.")

		return nil
	},
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func init() {
	rootCmd.AddCommand(configCmd)
}
