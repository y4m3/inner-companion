package agentdef

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFromFile_ValidYAML(t *testing.T) {
	yaml := `
name: test-agent
system_prompt: "You are helpful."
allowed_tools:
  - read
  - bash
autonomy: supervised
`
	path := writeTestFile(t, yaml)
	def, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Name != "test-agent" {
		t.Errorf("Name = %q, want %q", def.Name, "test-agent")
	}
	if def.SystemPrompt != "You are helpful." {
		t.Errorf("SystemPrompt = %q, want %q", def.SystemPrompt, "You are helpful.")
	}
	if len(def.AllowedTools) != 2 {
		t.Fatalf("AllowedTools length = %d, want 2", len(def.AllowedTools))
	}
	if def.AllowedTools[0] != "read" || def.AllowedTools[1] != "bash" {
		t.Errorf("AllowedTools = %v, want [read bash]", def.AllowedTools)
	}
	if def.Autonomy != AutonomySupervised {
		t.Errorf("Autonomy = %q, want %q", def.Autonomy, AutonomySupervised)
	}
}

func TestLoadFromFile_FileNotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path/agent.yaml")
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestLoadFromFile_InvalidYAMLSyntax(t *testing.T) {
	path := writeTestFile(t, "{{invalid yaml::")
	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestValidate_Valid(t *testing.T) {
	def := AgentDef{
		Name:         "test-agent",
		AllowedTools: []string{"read"},
		Autonomy:     AutonomyReadonly,
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	def := AgentDef{
		Autonomy: AutonomyFull,
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestValidate_InvalidAutonomy(t *testing.T) {
	def := AgentDef{
		Name:     "test",
		Autonomy: "invalid",
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for invalid autonomy, got nil")
	}
}

func TestValidate_UnknownTool(t *testing.T) {
	def := AgentDef{
		Name:         "test",
		AllowedTools: []string{"read", "unknown_tool"},
		Autonomy:     AutonomySupervised,
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}
