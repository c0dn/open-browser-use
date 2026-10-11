// Package policy runs a browser goal with TypeSafe's Jev: one System One request per step picks the operation and,
// speculatively, a target for each operation; a small chat model writes field values. Ported from Maya's browser
// policy (itself from browser-use/jev-ultrafast's model.py, MIT).
//
// Model output never becomes a selector or code: Jev can only choose among offered labels (operation names and the
// observed control indices), and the text model only supplies a string to type into a control Jev chose.
package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config says where the gateway is and which models to use.
type Config struct {
	// BaseURL is the gateway root, without /v1 (https://ai.c0dn.dev).
	BaseURL   string
	APIKey    string
	Model     string // System One decision model
	TextModel string // chat model that writes field values
	HTTP      *http.Client
}

const (
	defaultBaseURL   = "https://ai.c0dn.dev"
	defaultKeyFile   = "~/.config/opencode/secrets/proteus-api-key"
	defaultModel     = "jev-latest"
	defaultTextModel = "gpt-5.6-luna--fast"
)

// ConfigFromEnv reads OBU_POLICY_BASE_URL, OBU_POLICY_API_KEY (or OBU_POLICY_API_KEY_FILE), OBU_POLICY_MODEL and
// OBU_POLICY_TEXT_MODEL, defaulting to Proteus at ai.c0dn.dev with the key OpenCode uses.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:   strings.TrimRight(envOr("OBU_POLICY_BASE_URL", defaultBaseURL), "/"),
		Model:     envOr("OBU_POLICY_MODEL", defaultModel),
		TextModel: envOr("OBU_POLICY_TEXT_MODEL", defaultTextModel),
		APIKey:    os.Getenv("OBU_POLICY_API_KEY"),
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/v1")
	if cfg.APIKey == "" {
		path := expandHome(envOr("OBU_POLICY_API_KEY_FILE", defaultKeyFile))
		raw, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("no policy API key: set OBU_POLICY_API_KEY or OBU_POLICY_API_KEY_FILE (%s: %w)", path, err)
		}
		cfg.APIKey = strings.TrimSpace(string(raw))
	}
	if cfg.APIKey == "" {
		return cfg, errors.New("the policy API key is empty")
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}

func (c Config) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Usage is what one System One request cost.
type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

type systemOneResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   Usage                      `json:"usage"`
}

func (c Config) post(ctx context.Context, path string, body any, timeout time.Duration, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	res, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("gateway %s returned %d: %s", path, res.StatusCode, msg)
	}
	return json.Unmarshal(data, out)
}

func (c Config) systemOne(ctx context.Context, state any, questions map[string]any) (*systemOneResponse, error) {
	var out systemOneResponse
	err := c.post(ctx, "/typesafe/v1/systemone", map[string]any{"model": c.Model, "state": state, "questions": questions},
		20*time.Second, &out)
	return &out, err
}

// chat asks the text model through the Responses API and returns its text.
func (c Config) chat(ctx context.Context, system, user string) (string, error) {
	var out struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	body := map[string]any{
		"model": c.TextModel, "store": false, "instructions": system, "max_output_tokens": 600,
		"reasoning": map[string]any{"effort": "low"},
		"input":     []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": user}}}},
	}
	if err := c.post(ctx, "/v1/responses", body, 30*time.Second, &out); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, item := range out.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				b.WriteString(part.Text)
			}
		}
	}
	return b.String(), nil
}
