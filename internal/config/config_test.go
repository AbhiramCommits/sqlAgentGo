package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadDefaultsWithoutFile(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("base url = %q", c.LLM.BaseURL)
	}
	if c.LLM.Model != "gpt-4o" {
		t.Fatalf("model = %q", c.LLM.Model)
	}
	if c.LLM.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("api key env = %q", c.LLM.APIKeyEnv)
	}
	if c.LLM.MaxRetries != 3 {
		t.Fatalf("max retries = %d", c.LLM.MaxRetries)
	}
	if c.LLM.Temperature != 0 {
		t.Fatalf("temperature = %v", c.LLM.Temperature)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := writeConfig(t, `
llm:
  base_url: "http://localhost:11434/v1"
  model: "llama3.2:3b"
  api_key_env: "MY_KEY"
  max_retries: 5
  temperature: 0.2
pricing:
  - model: default
    prompt_usd_per_1m: 1.0
    completion_usd_per_1m: 2.0
  - model: llama3.2:3b
    prompt_usd_per_1m: 0.0
    completion_usd_per_1m: 0.0
`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.BaseURL != "http://localhost:11434/v1" || c.LLM.Model != "llama3.2:3b" {
		t.Fatalf("llm = %+v", c.LLM)
	}
	if c.LLM.APIKeyEnv != "MY_KEY" || c.LLM.MaxRetries != 5 || c.LLM.Temperature != 0.2 {
		t.Fatalf("llm = %+v", c.LLM)
	}
	if p := c.PriceFor("llama3.2:3b"); p.PromptUSDPer1M != 0 || p.CompletionUSDPer1M != 0 {
		t.Fatalf("model price = %+v", p)
	}
	if p := c.PriceFor("unknown-model"); p.PromptUSDPer1M != 1.0 || p.CompletionUSDPer1M != 2.0 {
		t.Fatalf("fallback price = %+v", p)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "http://example.test/v1")
	t.Setenv("LLM_MODEL", "env-model")
	t.Setenv("LLM_API_KEY_ENV", "OTHER_KEY")
	t.Setenv("LLM_MAX_RETRIES", "7")
	t.Setenv("LLM_TEMPERATURE", "0.5")
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.BaseURL != "http://example.test/v1" || c.LLM.Model != "env-model" ||
		c.LLM.APIKeyEnv != "OTHER_KEY" || c.LLM.MaxRetries != 7 || c.LLM.Temperature != 0.5 {
		t.Fatalf("env overrides not applied: %+v", c.LLM)
	}
}

func TestLoadEnvOverridesIgnoreGarbage(t *testing.T) {
	t.Setenv("LLM_MAX_RETRIES", "not-a-number")
	t.Setenv("LLM_TEMPERATURE", "also-bad")
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.MaxRetries != 3 || c.LLM.Temperature != 0 {
		t.Fatalf("garbage env should be ignored: %+v", c.LLM)
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("llm: [unbalanced"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected parse error for malformed config")
	}
}

func TestPriceForNoPricing(t *testing.T) {
	c := &Config{}
	if p := c.PriceFor("anything"); p.PromptUSDPer1M != 0 || p.CompletionUSDPer1M != 0 {
		t.Fatalf("empty pricing should yield zero price: %+v", p)
	}
}

func TestModelPriceCostUSD(t *testing.T) {
	p := ModelPrice{PromptUSDPer1M: 2.5, CompletionUSDPer1M: 10}
	got := p.CostUSD(1000000, 500000)
	want := 2.5 + 5.0
	if got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}
	if p.CostUSD(0, 0) != 0 {
		t.Fatal("zero tokens must cost zero")
	}
}
