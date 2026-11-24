// Package config reads settings from environment variables and an optional .env file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// ErrMissingKey is returned when GROQ_API_KEY is not set.
var ErrMissingKey = errors.New("GROQ_API_KEY is missing. Create a free key at https://console.groq.com/keys and put it in .env as GROQ_API_KEY=...")

// Config holds every knob of the help desk.
type Config struct {
	APIKey          string
	BaseURL         string // Groq's OpenAI-compatible endpoint
	Model           string
	TopK            int // passages the retriever hands to the evaluator
	WebResults      int // web snippets fetched when the KB is not enough
	GradeMaxTokens  int // gpt-oss spends completion tokens reasoning before the JSON
	AnswerMaxTokens int
	MaxRetries      int    // retries for 429/5xx from Groq
	KBPath          string // empty = embedded default knowledge base
	Addr            string // HTTP listen address
	MaxQuestionLen  int
	MaxConcurrent   int  // simultaneous pipeline runs the server allows
	RatePerMinute   int  // per-client questions per minute on /ask; 0 disables
	TrustProxy      bool // read the client IP from X-Forwarded-For (set behind a proxy like Render)
	HelpDeskName    string
	RewriteHint     string
	LogLevel        slog.Level
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		BaseURL:         "https://api.groq.com/openai/v1",
		Model:           "openai/gpt-oss-120b",
		TopK:            3,
		WebResults:      4,
		GradeMaxTokens:  2000,
		AnswerMaxTokens: 600,
		MaxRetries:      5,
		Addr:            ":8000",
		MaxQuestionLen:  500,
		MaxConcurrent:   4,
		RatePerMinute:   10,
		HelpDeskName:    "Indian Railways",
		RewriteHint:     `If the question is about trains, include "Indian Railways" or "IRCTC".`,
		LogLevel:        slog.LevelInfo,
	}
}

// Load reads .env (if present, never overriding real environment variables) and
// then the environment. It does not require the API key; call RequireKey for that.
func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}
	return FromEnv(os.Getenv)
}

// FromEnv builds a Config from any lookup function, which keeps it testable.
func FromEnv(get func(string) string) (Config, error) {
	c := Defaults()
	c.APIKey = strings.TrimSpace(get("GROQ_API_KEY"))
	setStr := func(dst *string, key string) {
		if v := strings.TrimSpace(get(key)); v != "" {
			*dst = v
		}
	}
	setStr(&c.BaseURL, "GROQ_BASE_URL")
	setStr(&c.Model, "CRAG_MODEL")
	setStr(&c.KBPath, "CRAG_KB_PATH")
	setStr(&c.Addr, "CRAG_ADDR")
	setStr(&c.HelpDeskName, "CRAG_HELPDESK_NAME")
	setStr(&c.RewriteHint, "CRAG_REWRITE_HINT")

	for key, dst := range map[string]*int{
		"CRAG_TOP_K":          &c.TopK,
		"CRAG_WEB_RESULTS":    &c.WebResults,
		"CRAG_MAX_CONCURRENT": &c.MaxConcurrent,
	} {
		if v := strings.TrimSpace(get(key)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return Config{}, fmt.Errorf("%s must be a positive integer, got %q", key, v)
			}
			*dst = n
		}
	}
	// Hosting platforms (Render, Railway, Cloud Run) hand us the port in PORT.
	if p := strings.TrimSpace(get("PORT")); p != "" && strings.TrimSpace(get("CRAG_ADDR")) == "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return Config{}, fmt.Errorf("PORT must be 1-65535, got %q", p)
		}
		c.Addr = ":" + p
	}
	if v := strings.TrimSpace(get("CRAG_RATE_PER_MINUTE")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("CRAG_RATE_PER_MINUTE must be 0 or a positive integer, got %q", v)
		}
		c.RatePerMinute = n
	}
	if v := strings.TrimSpace(get("CRAG_TRUST_PROXY")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("CRAG_TRUST_PROXY must be true or false, got %q", v)
		}
		c.TrustProxy = b
	}
	if v := strings.TrimSpace(get("CRAG_LOG_LEVEL")); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return Config{}, fmt.Errorf("CRAG_LOG_LEVEL: %w", err)
		}
	}
	return c, nil
}

// RequireKey returns the API key or an error that says how to get one.
func (c Config) RequireKey() (string, error) {
	if c.APIKey == "" {
		return "", ErrMissingKey
	}
	return c.APIKey, nil
}

// loadDotEnv sets variables from KEY=VALUE lines. A missing file is fine.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := ParseEnvLine(sc.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// ParseEnvLine parses one .env line. Comments, blanks and malformed lines return ok=false.
func ParseEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	} else if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return key, value, true
}
