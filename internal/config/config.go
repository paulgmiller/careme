package config

import (
	"os"
)

type Config struct {
	AI         AIConfig         `json:"ai"`
	Kroger     KrogerConfig     `json:"kroger"`
	Epicurious EpicuriousConfig `json:"epicurious"`
	History    HistoryConfig    `json:"history"`
}

type AIConfig struct {
	Provider string `json:"provider"` // "openai" or "anthropic"
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

type KrogerConfig struct {
	MCPServerURL string `json:"mcp_server_url"`
	APIKey       string `json:"api_key"`
}

type EpicuriousConfig struct {
	APIEndpoint string `json:"api_endpoint"`
	APIKey      string `json:"api_key"`
}

type HistoryConfig struct {
	StoragePath   string `json:"storage_path"`
	RetentionDays int    `json:"retention_days"`
}

func Load() (*Config, error) {
	config := &Config{
		AI: AIConfig{
			Provider: getEnvOrDefault("AI_PROVIDER", "openai"),
			APIKey:   os.Getenv("AI_API_KEY"),
			Model:    getEnvOrDefault("AI_MODEL", "gpt-4"),
		},
		Kroger: KrogerConfig{
			MCPServerURL: getEnvOrDefault("KROGER_MCP_URL", "http://localhost:8080"),
			APIKey:       os.Getenv("KROGER_API_KEY"),
		},
		Epicurious: EpicuriousConfig{
			APIEndpoint: getEnvOrDefault("EPICURIOUS_ENDPOINT", "https://api.epicurious.com"),
			APIKey:      os.Getenv("EPICURIOUS_API_KEY"),
		},
		History: HistoryConfig{
			StoragePath:   getEnvOrDefault("HISTORY_PATH", "./data/history.json"),
			RetentionDays: 14,
		},
	}

	return config, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}