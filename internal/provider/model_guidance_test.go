package provider

import "testing"

func TestGetModelToolGuidance_GemmaModel(t *testing.T) {
	// Gemma models are native-capable but not open-source prefixed,
	// so they should get no guidance (or Gemini guidance if gemini- prefix).
	g := getModelToolGuidance("google-ai-studio", "gemma-4-31b-it")
	if g != "" {
		t.Errorf("expected empty guidance for gemma, got non-empty")
	}
}

func TestGetModelToolGuidance_LlamaModel(t *testing.T) {
	g := getModelToolGuidance("google-ai-studio", "llama-3.1-70b")
	if g != openSourceToolGuidance {
		t.Error("expected open source guidance for llama")
	}
}

func TestGetModelToolGuidance_QwenModel(t *testing.T) {
	g := getModelToolGuidance("google-ai-studio", "qwen2-72b")
	if g != openSourceToolGuidance {
		t.Error("expected open source guidance for qwen")
	}
}

func TestGetModelToolGuidance_GeminiModel(t *testing.T) {
	g := getModelToolGuidance("google-ai-studio", "gemini-2.5-pro")
	if g != geminiToolGuidance {
		t.Error("expected gemini guidance")
	}
}

func TestGetModelToolGuidance_ClaudeModel(t *testing.T) {
	g := getModelToolGuidance("anthropic", "claude-sonnet-4-5")
	if g != claudeToolGuidance {
		t.Error("expected claude guidance")
	}
}

func TestGetModelToolGuidance_GPTModel(t *testing.T) {
	g := getModelToolGuidance("openai", "gpt-4o")
	if g != gptToolGuidance {
		t.Error("expected gpt guidance")
	}
}

func TestGetModelToolGuidance_OpenCodeProvider(t *testing.T) {
	g := getModelToolGuidance("opencode", "some-model")
	if g != gptToolGuidance {
		t.Error("expected gpt guidance for opencode provider")
	}
}

func TestGetModelToolGuidance_UnknownModel(t *testing.T) {
	g := getModelToolGuidance("custom", "totally-unknown-model")
	if g != "" {
		t.Errorf("expected empty guidance for unknown model, got non-empty")
	}
}

func TestGetModelToolGuidance_ModelsPrefix(t *testing.T) {
	g := getModelToolGuidance("google-ai-studio", "models/gemini-2.5-flash")
	if g != geminiToolGuidance {
		t.Error("expected gemini guidance even with models/ prefix")
	}
}
