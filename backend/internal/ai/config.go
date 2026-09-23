package ai

import (
	"fmt"
	"os"
	"strings"
)

// Config selects and configures the provider per environment.
type Config struct {
	Provider string // a preset name, or any label of your own alongside AI_BASE_URL
	APIKey   string
	BaseURL  string
	Model    string
}

// FromEnv reads the config from the environment (secrets outside the code -- law 2.6).
//
//	AI_PROVIDER   preset name, or any label (default "deepseek")
//	AI_API_KEY    required
//	AI_BASE_URL   required unless the provider is a preset; overrides the preset
//	AI_MODEL      required unless the preset carries a default; overrides the preset
//	AI_REASONING  "on" lets the model think first, where the endpoint supports it
func FromEnv() Config {
	return Config{
		Provider: getenv("AI_PROVIDER", "deepseek"),
		APIKey:   os.Getenv("AI_API_KEY"),
		BaseURL:  os.Getenv("AI_BASE_URL"),
		Model:    os.Getenv("AI_MODEL"),
	}
}

// A preset is a known endpoint, and nothing more: it saves a deployment from looking up an address
// it cannot guess. Presets are a convenience, NOT a list of what is supported -- any endpoint
// speaking the protocol works by naming it and giving AI_BASE_URL.
//
// Most carry no default model on purpose. A model name is the part of this that goes stale: the
// previous default here was `deepseek-chat`, which the vendor discontinued, and a default that
// stops existing fails at the first request rather than at startup. Where a default is kept it is
// because deployments already depend on it.
type preset struct {
	baseURL  string
	model    string
	thinking bool // the endpoint understands DeepSeek's `thinking` field
}

var presets = map[string]preset{
	"deepseek": {baseURL: "https://api.deepseek.com", model: "deepseek-v4-flash", thinking: true},
	// Same protocol, different jurisdiction: TensorX serves open models on European
	// infrastructure, which is what the subprocessors page rests on.
	// Pinned to a dated snapshot. The undated `deepseek/deepseek-v4-flash` was retired from the
	// catalogue on 2026-09-23 and every request failed with a 403 until AI_MODEL was changed; a
	// snapshot is retired too, eventually, and CheckModel says so at startup when it is.
	"tensorx": {baseURL: "https://api.tensorx.ai/v1", model: "deepseek/deepseek-v4-flash-0731", thinking: true},
	"openai":  {baseURL: "https://api.openai.com/v1"},
	// A local runtime, for an installation that wants no model traffic leaving the machine.
	"ollama": {baseURL: "http://localhost:11434/v1"},
}

// New builds the Provider from the config.
//
// There is no list of accepted providers. There was, and it rejected every name but two, so an
// installation could not point Beskos at OpenAI, Mistral, Groq, OpenRouter, vLLM or a model on its
// own machine -- all of which speak the protocol already implemented here.
//
// What is missing is named exactly, because "unknown provider" told an operator nothing about what
// to do next (law 2.9).
func New(cfg Config) (Provider, error) {
	name := strings.TrimSpace(cfg.Provider)
	if name == "" {
		name = "openai-compatible"
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("ai: AI_API_KEY is required")
	}

	p, known := presets[name]
	base, model := cfg.BaseURL, cfg.Model
	if base == "" {
		base = p.baseURL
	}
	if model == "" {
		model = p.model
	}

	if base == "" {
		return nil, fmt.Errorf("ai: AI_BASE_URL is required for provider %q (it is not a known preset: %s)",
			name, presetNames())
	}
	if model == "" {
		return nil, fmt.Errorf("ai: AI_MODEL is required for provider %q", name)
	}

	return NewClient(ClientOptions{
		Name:     name,
		APIKey:   cfg.APIKey,
		BaseURL:  base,
		Model:    model,
		Thinking: known && p.thinking,
	}), nil
}

func presetNames() string {
	names := make([]string, 0, len(presets))
	for n := range presets {
		names = append(names, n)
	}
	// Sorted so the error message is the same every time it is read.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return strings.Join(names, ", ")
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
