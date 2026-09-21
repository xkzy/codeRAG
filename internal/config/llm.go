package config

import "time"

type LLMProvider string

const (
	ProviderOllama  LLMProvider = "ollama"
	ProviderOpenAI  LLMProvider = "openai"
	ProviderLocal   LLMProvider = "local"
)

// LLMConfig configures offloading simple tasks to small models.
type LLMConfig struct {
	// Enabled controls whether small-model offloading is active.
	Enabled bool `yaml:"enabled" xml:"enabled"`

	// Provider is the backend: "ollama", "openai", or "local".
	Provider LLMProvider `yaml:"provider" xml:"provider"`

	// Endpoint is the API URL (e.g., http://localhost:11434/api/chat for Ollama).
	Endpoint string `yaml:"endpoint" xml:"endpoint"`

	// Model is the model name (e.g., "phi3:mini", "gemma2:latest", "gpt-3.5-turbo").
	Model string `yaml:"model" xml:"model"`

	// APIKey for providers that require authentication (OpenAI).
	APIKey string `yaml:"api_key" xml:"api_key,omitempty"`

	// MaxTokens is the token budget for a single offloaded task.
	// Tasks exceeding this budget are kept for the main model.
	MaxTokens int `yaml:"max_tokens" xml:"max_tokens"`

	// Timeout for the small model request.
	Timeout time.Duration `yaml:"timeout" xml:"timeout"`
}

func DefaultLLMConfig() LLMConfig {
	return LLMConfig{
		Enabled:   false,
		Provider:  ProviderOllama,
		Endpoint:  "http://localhost:11434/api/chat",
		Model:     "phi3:mini",
		MaxTokens: 500,
		Timeout:   30 * time.Second,
	}
}
