// Package config loads sqlagent configuration via viper.
package config

import (
	"errors"
	"os"
	"strconv"

	"github.com/spf13/viper"
)

type Config struct {
	LLM     LLM            `mapstructure:"llm"`
	Pricing []PricingEntry `mapstructure:"pricing"`
}

// PricingEntry is the price for one model. A list (not a map) keeps viper
// from treating dots in model names (llama3.2:3b) as nested keys.
type PricingEntry struct {
	Model              string  `mapstructure:"model"`
	PromptUSDPer1M     float64 `mapstructure:"prompt_usd_per_1m"`
	CompletionUSDPer1M float64 `mapstructure:"completion_usd_per_1m"`
}

// ModelPrice is the USD cost per 1M tokens for one model.
type ModelPrice struct {
	PromptUSDPer1M     float64
	CompletionUSDPer1M float64
}

// PriceFor returns the price table entry for model, falling back to the
// "default" entry and then to zero (e.g. local models).
func (c *Config) PriceFor(model string) ModelPrice {
	var fallback ModelPrice
	for _, e := range c.Pricing {
		if e.Model == "default" {
			fallback = ModelPrice{PromptUSDPer1M: e.PromptUSDPer1M, CompletionUSDPer1M: e.CompletionUSDPer1M}
		}
		if e.Model == model {
			return ModelPrice{PromptUSDPer1M: e.PromptUSDPer1M, CompletionUSDPer1M: e.CompletionUSDPer1M}
		}
	}
	return fallback
}

// CostUSD estimates the dollar cost of one run given token counts.
func (p ModelPrice) CostUSD(promptTokens, completionTokens int) float64 {
	return float64(promptTokens)/1e6*p.PromptUSDPer1M +
		float64(completionTokens)/1e6*p.CompletionUSDPer1M
}

type LLM struct {
	BaseURL     string  `mapstructure:"base_url"`
	Model       string  `mapstructure:"model"`
	APIKeyEnv   string  `mapstructure:"api_key_env"`
	MaxRetries  int     `mapstructure:"max_retries"`
	Temperature float64 `mapstructure:"temperature"`
}

// Load reads config files named "config" (yaml/json/...) from each path in
// searchPaths and merges them with defaults. Missing config files are not an
// error; unreadable ones are.
func Load(searchPaths ...string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	for _, p := range searchPaths {
		v.AddConfigPath(p)
	}
	v.SetDefault("llm.base_url", "https://api.openai.com/v1")
	v.SetDefault("llm.model", "gpt-4o")
	v.SetDefault("llm.api_key_env", "OPENAI_API_KEY")
	v.SetDefault("llm.max_retries", 3)
	v.SetDefault("llm.temperature", 0)
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, err
		}
	}

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, err
	}
	// viper's AutomaticEnv does not flow through Unmarshal for keys present
	// in the config file, so apply explicit environment overrides here.
	// These make `eval` swappable per-run, e.g.
	// LLM_BASE_URL=http://localhost:11434/v1 LLM_MODEL=llama3.2:3b.
	if s := os.Getenv("LLM_BASE_URL"); s != "" {
		c.LLM.BaseURL = s
	}
	if s := os.Getenv("LLM_MODEL"); s != "" {
		c.LLM.Model = s
	}
	if s := os.Getenv("LLM_API_KEY_ENV"); s != "" {
		c.LLM.APIKeyEnv = s
	}
	if s := os.Getenv("LLM_MAX_RETRIES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			c.LLM.MaxRetries = n
		}
	}
	if s := os.Getenv("LLM_TEMPERATURE"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			c.LLM.Temperature = f
		}
	}
	return &c, nil
}
