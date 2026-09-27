// Package config loads sqlagent configuration via viper.
package config

import (
	"errors"

	"github.com/spf13/viper"
)

type Config struct {
	LLM LLM `mapstructure:"llm"`
}

type LLM struct {
	BaseURL    string `mapstructure:"base_url"`
	Model      string `mapstructure:"model"`
	APIKeyEnv  string `mapstructure:"api_key_env"`
	MaxRetries int    `mapstructure:"max_retries"`
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
	return &c, nil
}
