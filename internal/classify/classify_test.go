package classify

import "testing"

func TestClassify_L1Commands(t *testing.T) {
	l1Commands := []string{
		"ls",
		"ls -la",
		"cat file.txt",
		"head -n 10 file.txt",
		"tail -f log.txt",
		"find . -name '*.go'",
		"rg pattern",
		"git status",
		"git diff",
		"git diff HEAD~1",
		"git log",
		"git log --oneline",
	}
	for _, cmd := range l1Commands {
		t.Run(cmd, func(t *testing.T) {
			result := Classify(cmd)
			if result.Level != L1 {
				t.Errorf("Classify(%q).Level = %d, want %d (L1)", cmd, result.Level, L1)
			}
			if result.Command != cmd {
				t.Errorf("Classify(%q).Command = %q, want %q", cmd, result.Command, cmd)
			}
		})
	}
}

func TestClassify_L2Commands(t *testing.T) {
	l2Commands := []string{
		"git checkout main",
		"git add .",
		"go test ./...",
		"npm install",
		"mkdir -p foo/bar",
		"cp file1 file2",
	}
	for _, cmd := range l2Commands {
		t.Run(cmd, func(t *testing.T) {
			result := Classify(cmd)
			if result.Level != L2 {
				t.Errorf("Classify(%q).Level = %d, want %d (L2)", cmd, result.Level, L2)
			}
		})
	}
}

func TestClassify_L3Commands(t *testing.T) {
	l3Commands := []string{
		"rm -rf /",
		"rm -rf .",
		"git push origin main",
		"git reset --hard HEAD",
		"chmod -R 777 /",
	}
	for _, cmd := range l3Commands {
		t.Run(cmd, func(t *testing.T) {
			result := Classify(cmd)
			if result.Level != L3 {
				t.Errorf("Classify(%q).Level = %d, want %d (L3)", cmd, result.Level, L3)
			}
		})
	}
}

func TestClassify_UnknownCommandDefaultsToL2(t *testing.T) {
	unknowns := []string{
		"curl https://example.com",
		"wget http://example.com",
		"python3 script.py",
	}
	for _, cmd := range unknowns {
		t.Run(cmd, func(t *testing.T) {
			result := Classify(cmd)
			if result.Level != L2 {
				t.Errorf("Classify(%q).Level = %d, want %d (L2, fail-closed)", cmd, result.Level, L2)
			}
		})
	}
}

func TestClassify_PipeL1(t *testing.T) {
	result := Classify("ls | grep foo")
	if result.Level != L1 {
		t.Errorf("Classify(\"ls | grep foo\").Level = %d, want %d (L1)", result.Level, L1)
	}
}

func TestClassify_CompoundHighestWins(t *testing.T) {
	result := Classify("ls && rm -rf /")
	if result.Level != L3 {
		t.Errorf("Classify(\"ls && rm -rf /\").Level = %d, want %d (L3)", result.Level, L3)
	}
}

func TestClassify_MixedCompound(t *testing.T) {
	result := Classify("cat file.txt; git push origin main")
	if result.Level != L3 {
		t.Errorf("Classify(\"cat file.txt; git push origin main\").Level = %d, want %d (L3)", result.Level, L3)
	}
}

func TestClassify_CompoundOrOperator(t *testing.T) {
	result := Classify("git status || git push origin main")
	if result.Level != L3 {
		t.Errorf("Classify(\"git status || git push origin main\").Level = %d, want %d (L3)", result.Level, L3)
	}
}
