package agentdef

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Autonomy string

const (
	AutonomyReadonly   Autonomy = "readonly"
	AutonomySupervised Autonomy = "supervised"
	AutonomyFull       Autonomy = "full"
)

var validAutonomy = map[Autonomy]bool{
	AutonomyReadonly:   true,
	AutonomySupervised: true,
	AutonomyFull:       true,
}

var knownTools = map[string]bool{
	"read": true,
	"bash": true,
}

type AgentDef struct {
	Name         string   `yaml:"name"`
	SystemPrompt string   `yaml:"system_prompt"`
	AllowedTools []string `yaml:"allowed_tools"`
	Autonomy     Autonomy `yaml:"autonomy"`
}

func LoadFromFile(path string) (AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AgentDef{}, fmt.Errorf("agentdef: %w", err)
	}
	var def AgentDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return AgentDef{}, fmt.Errorf("agentdef: invalid yaml: %w", err)
	}
	return def, nil
}

func (d AgentDef) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("agentdef: name is required")
	}
	if !validAutonomy[d.Autonomy] {
		return fmt.Errorf("agentdef: invalid autonomy: %q", d.Autonomy)
	}
	for _, t := range d.AllowedTools {
		if !knownTools[t] {
			return fmt.Errorf("agentdef: unknown tool: %q", t)
		}
	}
	return nil
}
