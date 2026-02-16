package classify

import (
	"strings"
)

// Level represents the security classification of a command.
type Level int

const (
	L1 Level = 1 // read-only, safe
	L2 Level = 2 // modifying, requires supervision
	L3 Level = 3 // dangerous, always denied
)

// ClassifyResult holds the classification outcome.
type ClassifyResult struct {
	Level   Level
	Command string
}

var l3Patterns = []string{
	"rm -rf", "git push", "git reset --hard", "chmod -R",
}

var l2Patterns = []string{
	"git checkout", "git add", "go test", "npm install", "mkdir", "cp",
}

var l1Patterns = []string{
	"ls", "cat", "head", "tail", "find", "grep", "rg", "git status", "git diff", "git log",
}

// Classify determines the security level of a shell command.
// Compound commands (joined by |, &&, ||, ;) are split and the highest level wins.
func Classify(command string) ClassifyResult {
	parts := splitCompound(command)
	maxLevel := L1
	for _, part := range parts {
		level := classifySingle(strings.TrimSpace(part))
		if level > maxLevel {
			maxLevel = level
		}
	}
	return ClassifyResult{Level: maxLevel, Command: command}
}

func splitCompound(cmd string) []string {
	var parts []string
	current := cmd
	for {
		idx := -1
		sep := ""
		for _, s := range []string{"&&", "||", "|", ";"} {
			i := strings.Index(current, s)
			if i >= 0 && (idx < 0 || i < idx) {
				idx = i
				sep = s
			}
		}
		if idx < 0 {
			parts = append(parts, current)
			break
		}
		parts = append(parts, current[:idx])
		current = current[idx+len(sep):]
	}
	return parts
}

func classifySingle(cmd string) Level {
	for _, p := range l3Patterns {
		if matchesPattern(cmd, p) {
			return L3
		}
	}
	for _, p := range l1Patterns {
		if matchesPattern(cmd, p) {
			return L1
		}
	}
	for _, p := range l2Patterns {
		if matchesPattern(cmd, p) {
			return L2
		}
	}
	return L2 // fail-closed: unknown commands default to L2
}

func matchesPattern(cmd, pattern string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == pattern {
		return true
	}
	if strings.HasPrefix(cmd, pattern+" ") {
		return true
	}
	return false
}
