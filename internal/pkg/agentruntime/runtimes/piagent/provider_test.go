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
			source, err := agentruntime.PiAgentProviderExtension(tc.p)
			if err != nil {
				t.Fatalf("PiAgentProviderExtension: %v", err)
			}
			path, err := materializeProviderExtension(source)
			if err != nil {
				t.Fatalf("materializeProviderExtension: %v", err)
			}
			wantDir := filepath.Join(dataDir, "piagent", "ext")
			if !strings.HasPrefix(path, wantDir) {
				t.Fatalf("path not under %s: %s", wantDir, path)
			}
			base := filepath.Base(path)
			if !strings.HasPrefix(base, "agentre-provider-") || !strings.HasSuffix(base, ".mjs") {
				t.Fatalf("unexpected filename: %s", base)
			}
			raw, err := os.ReadFile(path) //nolint:gosec // path returned by materializeProviderExtension, constrained to the test temp data dir.
			if err != nil {
				t.Fatalf("read: %v", err)
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
	source := "export default function (pi) { pi.registerProvider(\"x\", {}) }"
	p1, err := materializeProviderExtension(source)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatalf("file missing: %v", err)
	}
	p2, err := materializeProviderExtension(source)
	if err != nil || p2 != p1 {
		t.Fatalf("not idempotent: p1=%s p2=%s err=%v", p1, p2, err)
	}
}

func TestMaterializeProviderExtension_DifferentSourceDifferentPath(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	p1, err := materializeProviderExtension("source one")
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	p2, err := materializeProviderExtension("source two")
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("expected different hashed paths, both %s", p1)
	}
}
