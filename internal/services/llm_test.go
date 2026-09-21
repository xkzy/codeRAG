package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codergag/internal/config"
)

func TestNewSmallModelService(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	s := NewSmallModelService(cfg)
	if s == nil {
		t.Fatal("expected non-nil service")
	}
	if !s.Enabled() == cfg.Enabled {
		t.Error("Enabled() mismatch")
	}
}

func TestSmallModelService_Enabled(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	s := NewSmallModelService(cfg)

	if !s.Enabled() {
		t.Error("expected enabled")
	}

	cfg.Enabled = false
	s2 := NewSmallModelService(cfg)
	if s2.Enabled() {
		t.Error("expected enabled after enabling")
	}
}

func TestClassifyTask_Disabled(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	s := NewSmallModelService(cfg)
	cfg.Enabled = false

	ok, reason := s.ClassifyTask("what does this function do")
	if ok {
		t.Error("expected false when disabled")
	}
	if reason != "disabled" {
		t.Errorf("expected 'disabled', got %q", reason)
	}
}

func TestClassifyTask_ComplexQuery(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	s := NewSmallModelService(cfg)

	// Query with complex patterns should not be offloaded
	ok, reason := s.ClassifyTask("trace the data flow from main through the call graph to verify behavioral equivalence")
	if ok {
		t.Error("expected false for complex query")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestClassifyTask_TooManyTokens(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	cfg.MaxTokens = 40
	s := NewSmallModelService(cfg)

	longQuery := ""
	for i := 0; i < 30; i++ {
		longQuery += "word "
	}

	ok, reason := s.ClassifyTask(longQuery)
	if ok {
		t.Error("expected false for too-long query")
	}
	if reason != "too many tokens" {
		t.Errorf("expected 'too many tokens', got %q", reason)
	}
}

func TestClassifyTask_SimpleQuery(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	s := NewSmallModelService(cfg)

	ok, reason := s.ClassifyTask("what is this function")
	if !ok {
		t.Error("expected true for simple query")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestClassifyTask_ShortQuery(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	s := NewSmallModelService(cfg)

	ok, _ := s.ClassifyTask("list all")
	if !ok {
		t.Error("expected true for short query with simple pattern")
	}
}

func TestClassifyTask_ComplexPattern(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	cfg.MaxTokens = 500
	s := NewSmallModelService(cfg)

	ok, reason := s.ClassifyTask("perform circular dependency analysis on these modules")
	if ok {
		t.Error("expected false for complex pattern")
	}
	if reason != "complex pattern: circular" {
		t.Errorf("expected circular pattern reason, got %q", reason)
	}
}

func TestSmallAsk_Disabled(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	s := NewSmallModelService(cfg)

	_, err := s.SmallAsk(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when disabled")
	}
	if err.Error() != "small model offloading is not enabled" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSmallAsk_OllamaSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model": "phi3:mini",
			"message": map[string]string{
				"role":    "assistant",
				"content": "Test response from small model",
			},
		})
	}))
	defer srv.Close()

	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	cfg.Provider = config.ProviderOllama
	cfg.Endpoint = srv.URL
	cfg.Model = "phi3:mini"
	cfg.Timeout = 5 * time.Second

	s := NewSmallModelService(cfg)
	resp, err := s.SmallAsk(context.Background(), "what is this function")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "Test response from small model" {
		t.Errorf("expected test response, got %q", resp)
	}
}

func TestSmallAsk_OllamaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	cfg.Provider = config.ProviderOllama
	cfg.Endpoint = srv.URL
	cfg.Timeout = 5 * time.Second

	s := NewSmallModelService(cfg)
	_, err := s.SmallAsk(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestSmallAsk_ConnectionError(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	cfg.Provider = config.ProviderOllama
	cfg.Endpoint = "http://127.0.0.1:1"
	cfg.Timeout = 1 * time.Second

	s := NewSmallModelService(cfg)
	_, err := s.SmallAsk(context.Background(), "test")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestSmallAsk_OpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gpt-3.5-turbo",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "OpenAI response",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer srv.Close()

	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	cfg.Provider = config.ProviderOpenAI
	cfg.Endpoint = srv.URL
	cfg.APIKey = "test-key"
	cfg.Timeout = 5 * time.Second

	s := NewSmallModelService(cfg)
	resp, err := s.SmallAsk(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "OpenAI response" {
		t.Errorf("expected OpenAI response, got %q", resp)
	}
}

func TestSmallAsk_UnsupportedProvider(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	cfg.Provider = "unsupported"

	s := NewSmallModelService(cfg)
	_, err := s.SmallAsk(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestSuggestSmallModel(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	s := NewSmallModelService(cfg)

	result, err := s.SuggestSmallModel("test-project", "what is this function")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["can_offload"] != true {
		t.Error("expected can_offload to be true")
	}
	if result["project_id"] != "test-project" {
		t.Errorf("expected project_id test-project, got %v", result["project_id"])
	}
	if result["provider"] != config.ProviderOllama {
		t.Errorf("expected provider ollama, got %v", result["provider"])
	}
	if result["model"] != "phi3:mini" {
		t.Errorf("expected model phi3:mini, got %v", result["model"])
	}
	if result["max_tokens"] != 500 {
		t.Errorf("expected max_tokens 500, got %v", result["max_tokens"])
	}
}

func TestSuggestSmallModel_Disabled(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	s := NewSmallModelService(cfg)

	result, err := s.SuggestSmallModel("test-project", "what is this function")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["can_offload"] != false {
		t.Error("expected can_offload to be false when disabled")
	}
	if result["reason"] != "disabled" {
		t.Errorf("expected reason 'disabled', got %v", result["reason"])
	}
}

func TestSmallModelService_Config(t *testing.T) {
	cfg := config.DefaultLLMConfig()
	cfg.Enabled = true
	cfg.Model = "gemma2:latest"
	cfg.MaxTokens = 200
	s := NewSmallModelService(cfg)

	got := s.Config()
	if got.Model != "gemma2:latest" {
		t.Errorf("expected model gemma2:latest, got %s", got.Model)
	}
	if got.MaxTokens != 200 {
		t.Errorf("expected max_tokens 200, got %d", got.MaxTokens)
	}
}
