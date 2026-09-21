package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codergag/internal/config"
)

type SmallModelService struct {
	cfg config.LLMConfig
}

func NewSmallModelService(cfg config.LLMConfig) *SmallModelService {
	return &SmallModelService{cfg: cfg}
}

func (s *SmallModelService) Config() config.LLMConfig {
	return s.cfg
}

func (s *SmallModelService) Enabled() bool {
	return s.cfg.Enabled
}

// ClassifyTask determines if a query is simple enough for a small model.
// Heuristics:
// - Query is under MaxTokens words
// - Query contains only simple patterns (no nested logic, no complex data flow)
// - Query asks for basic operations: naming, categorization, simple extraction
func (s *SmallModelService) ClassifyTask(prompt string) (bool, string) {
	if !s.cfg.Enabled {
		return false, "disabled"
	}

	wordCount := len(strings.Fields(prompt))
	maxWords := s.cfg.MaxTokens / 4
	if wordCount > maxWords {
		return false, "too many tokens"
	}

	// Simple patterns that are safe for small models
	simplePatterns := []string{
		"what is this function", "what does", "name the functions",
		"extract", "list all", "count the", "summarize",
		"categorize", "classify", "label", "identify the",
		"simple", "basic", "quick",
	}

	lowerPrompt := strings.ToLower(prompt)
	for _, p := range simplePatterns {
		if strings.Contains(lowerPrompt, p) {
			return true, "simple pattern: " + p
		}
	}

	// Too complex (mentions of flow, tracing, complex analysis)
	complexPatterns := []string{
		"trace", "data flow", "taint", "call graph", "call path",
		"behavioral equivalence", "symbolic execution", "vulnerability",
		"impact analysis", "circular",
	}
	for _, p := range complexPatterns {
		if strings.Contains(lowerPrompt, p) {
			return false, "complex pattern: " + p
		}
	}

	if wordCount < 30 {
		return true, "short query"
	}

	return false, "query too complex for small model"
}

// SmallAsk sends a simple query to the small model and returns the response.
func (s *SmallModelService) SmallAsk(ctx context.Context, prompt string) (string, error) {
	if !s.cfg.Enabled {
		return "", fmt.Errorf("small model offloading is not enabled")
	}

	client := &http.Client{Timeout: s.cfg.Timeout}

	// Build request based on provider
	var req *http.Request
	var err error

	body := s.buildRequestBody(prompt)

	switch s.cfg.Provider {
	case config.ProviderOllama:
		req, err = http.NewRequestWithContext(ctx, "POST", s.cfg.Endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

	case config.ProviderOpenAI:
		req, err = http.NewRequestWithContext(ctx, "POST", s.cfg.Endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		if s.cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
		}

	default:
		return "", fmt.Errorf("unsupported provider: %s", s.cfg.Provider)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("small model request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("small model returned status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return s.parseResponse(respBody), nil
}

type ollamaRequest struct {
	Model    string                    `json:"model"`
	Messages []map[string]string       `json:"messages"`
	Stream   bool                      `json:"stream"`
	Options  map[string]any            `json:"options,omitempty"`
	Format   map[string]any            `json:"format,omitempty"`
}

func (s *SmallModelService) buildRequestBody(prompt string) []byte {
	switch s.cfg.Provider {
	case config.ProviderOllama:
		req := ollamaRequest{
			Model: s.cfg.Model,
			Messages: []map[string]string{
				{"role": "system", "content": "You are a helpful coding assistant. Answer concisely."},
				{"role": "user", "content": prompt},
			},
			Stream:  false,
			Options: map[string]any{"num_ctx": s.cfg.MaxTokens},
		}
		b, _ := json.Marshal(req)
		return b
	case config.ProviderOpenAI:
		req := map[string]any{
			"model":    s.cfg.Model,
			"messages": []map[string]string{
				{"role": "system", "content": "You are a helpful coding assistant. Answer concisely."},
				{"role": "user", "content": prompt},
			},
			"max_tokens": s.cfg.MaxTokens,
		}
		b, _ := json.Marshal(req)
		return b
	default:
		return nil
	}
}

func (s *SmallModelService) parseResponse(body []byte) string {
	switch s.cfg.Provider {
	case config.ProviderOllama:
		var resp struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(body, &resp); err == nil {
			return resp.Message.Content
		}
	case config.ProviderOpenAI:
		var resp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && len(resp.Choices) > 0 {
			return resp.Choices[0].Message.Content
		}
	}
	return string(body)
}

// SuggestSmallModel returns a recommendation on whether to use the small model.
// This is the primary MCP handler integration point.
func (s *SmallModelService) SuggestSmallModel(projectID, task string) (map[string]any, error) {
	ok, reason := s.ClassifyTask(task)
	return map[string]any{
		"can_offload":  ok,
		"reason":       reason,
		"provider":     s.cfg.Provider,
		"model":        s.cfg.Model,
		"max_tokens":   s.cfg.MaxTokens,
		"project_id":   projectID,
	}, nil
}

var _ = time.Second
