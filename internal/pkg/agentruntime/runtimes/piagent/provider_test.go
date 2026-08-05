package piagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

func TestMaterializeProviderExtension_WritesContentHashedFile(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	tcs := []struct {
		name string
		p    *llm_provider_entity.LLMProvider
	}{
		{
			name: "anthropic",
			p: &llm_provider_entity.LLMProvider{
				ProviderKey: "prov-a", Name: "ProvA", BaseURL: "https://a.example",
				Type: string(llm_provider_entity.TypeAnthropic), Model: "claude-3",
				APIKey: "tok-ant-aa", ContextWindow: 200000, MaxOutput: 8192,
			},
		},
		{
			name: "openai-chat",
			p: &llm_provider_entity.LLMProvider{
				ProviderKey: "prov-b", Name: "ProvB", BaseURL: "https://b.example",
				Type: string(llm_provider_entity.TypeOpenAIChat), Model: "gpt-4o",
				APIKey: "tok-bb",
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			path, err := MaterializeProviderExtension(tc.p)
			if err != nil {
				t.Fatalf("MaterializeProviderExtension: %v", err)
			}
			wantDir := filepath.Join(dataDir, "piagent", "ext")
			if !strings.HasPrefix(path, wantDir) {
				t.Fatalf("path not under %s: %s", wantDir, path)
			}
			base := filepath.Base(path)
			if !strings.HasPrefix(base, "agentre-provider-") || !strings.HasSuffix(base, ".mjs") {
				t.Fatalf("unexpected filename: %s", base)
			}
			raw, err := os.ReadFile(path) //nolint:gosec // path returned by MaterializeProviderExtension, constrained to the test temp data dir.
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			source, err := agentruntime.PiAgentProviderExtension(tc.p)
			if err != nil {
				t.Fatalf("PiAgentProviderExtension: %v", err)
			}
			if string(raw) != source {
				t.Fatalf("file content != rendered source")
			}
			// 密钥永不落盘（决策 #4）：扩展文件只含 $ENV_VAR 引用，绝不含明文 APIKey。
			if strings.Contains(string(raw), tc.p.APIKey) {
				t.Fatalf("extension leaked APIKey literal")
			}
		})
	}
}

func TestMaterializeProviderExtension_Idempotent(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	p := &llm_provider_entity.LLMProvider{
		ProviderKey: "prov-x", Name: "ProvX", BaseURL: "https://x.example",
		Type: string(llm_provider_entity.TypeOpenAIChat), Model: "gpt-4o", APIKey: "tok-xx",
	}
	p1, err := MaterializeProviderExtension(p)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatalf("file missing: %v", err)
	}
	p2, err := MaterializeProviderExtension(p)
	if err != nil || p2 != p1 {
		t.Fatalf("not idempotent: p1=%s p2=%s err=%v", p1, p2, err)
	}
}

func TestMaterializeProviderExtension_DifferentSourceDifferentPath(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	base := func(model string) *llm_provider_entity.LLMProvider {
		return &llm_provider_entity.LLMProvider{
			ProviderKey: "prov-y", Name: "ProvY", BaseURL: "https://y.example",
			Type: string(llm_provider_entity.TypeOpenAIResponse), Model: model, APIKey: "tok-yy",
		}
	}
	p1, err := MaterializeProviderExtension(base("model-one"))
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	p2, err := MaterializeProviderExtension(base("model-two"))
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("expected different hashed paths, both %s", p1)
	}
}
