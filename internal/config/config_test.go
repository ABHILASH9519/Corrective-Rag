package config_test

import (
	"errors"
	"log/slog"
	"testing"

	"crag/internal/config"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := config.FromEnv(env(nil))
	if err != nil || c.Model != "openai/gpt-oss-120b" || c.TopK != 3 || c.Addr != ":8000" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := c.RequireKey(); !errors.Is(err, config.ErrMissingKey) {
		t.Fatalf("want ErrMissingKey, got %v", err)
	}
}

func TestOverrides(t *testing.T) {
	c, err := config.FromEnv(env(map[string]string{
		"GROQ_API_KEY": " k ", "CRAG_MODEL": "m", "CRAG_TOP_K": "5", "CRAG_LOG_LEVEL": "debug", "CRAG_ADDR": ":9",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if k, _ := c.RequireKey(); k != "k" || c.Model != "m" || c.TopK != 5 || c.Addr != ":9" || c.LogLevel != slog.LevelDebug {
		t.Fatalf("%+v", c)
	}
}

func TestInvalidValues(t *testing.T) {
	for _, m := range []map[string]string{
		{"CRAG_TOP_K": "abc"}, {"CRAG_TOP_K": "0"}, {"CRAG_WEB_RESULTS": "-1"}, {"CRAG_LOG_LEVEL": "loud"},
	} {
		if _, err := config.FromEnv(env(m)); err == nil {
			t.Errorf("%v: expected error", m)
		}
	}
}

func TestPortAndRateSettings(t *testing.T) {
	c, err := config.FromEnv(env(map[string]string{"PORT": "10000", "CRAG_RATE_PER_MINUTE": "0", "CRAG_TRUST_PROXY": "true"}))
	if err != nil || c.Addr != ":10000" || c.RatePerMinute != 0 || !c.TrustProxy {
		t.Fatalf("%+v %v", c, err)
	}
	// An explicit CRAG_ADDR beats PORT.
	c, _ = config.FromEnv(env(map[string]string{"PORT": "10000", "CRAG_ADDR": ":9"}))
	if c.Addr != ":9" || c.RatePerMinute != 10 {
		t.Fatalf("%+v", c)
	}
	for _, m := range []map[string]string{
		{"PORT": "abc"}, {"PORT": "70000"}, {"CRAG_RATE_PER_MINUTE": "-1"}, {"CRAG_TRUST_PROXY": "maybe"},
	} {
		if _, err := config.FromEnv(env(m)); err == nil {
			t.Errorf("%v: expected error", m)
		}
	}
}

func TestParseEnvLine(t *testing.T) {
	cases := []struct {
		line, key, val string
		ok             bool
	}{
		{"A=b", "A", "b", true},
		{` export A = "b c" `, "A", "b c", true},
		{"A='x'", "A", "x", true},
		{"A=b # comment", "A", "b", true},
		{"A=", "A", "", true},
		{"# c", "", "", false},
		{"", "", "", false},
		{"novalue", "", "", false},
		{"=x", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := config.ParseEnvLine(c.line)
		if k != c.key || v != c.val || ok != c.ok {
			t.Errorf("%q: got %q %q %v", c.line, k, v, ok)
		}
	}
}
