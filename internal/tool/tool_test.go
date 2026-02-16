package tool

import (
	"context"
	"sort"
	"testing"
)

// stubExecutor is a minimal Executor for testing the Registry.
type stubExecutor struct {
	name string
}

func (s stubExecutor) Name() string { return s.name }

func (s stubExecutor) Execute(ctx context.Context, args map[string]any) Result {
	return Result{Output: "stub:" + s.name, IsError: false}
}

func TestNewRegistryWithMultipleExecutors(t *testing.T) {
	a := stubExecutor{name: "alpha"}
	b := stubExecutor{name: "beta"}
	r := NewRegistry(a, b)

	if len(r.tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(r.tools))
	}
}

func TestGetReturnsCorrectExecutor(t *testing.T) {
	a := stubExecutor{name: "alpha"}
	b := stubExecutor{name: "beta"}
	r := NewRegistry(a, b)

	got, ok := r.Get("alpha")
	if !ok {
		t.Fatal("expected Get('alpha') to return true")
	}
	if got.Name() != "alpha" {
		t.Fatalf("expected executor name 'alpha', got %q", got.Name())
	}

	got, ok = r.Get("beta")
	if !ok {
		t.Fatal("expected Get('beta') to return true")
	}
	if got.Name() != "beta" {
		t.Fatalf("expected executor name 'beta', got %q", got.Name())
	}
}

func TestGetUnknownNameReturnsFalse(t *testing.T) {
	r := NewRegistry(stubExecutor{name: "alpha"})

	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected Get('nonexistent') to return false")
	}
}

func TestHasReturnsTrueAndFalse(t *testing.T) {
	r := NewRegistry(stubExecutor{name: "alpha"})

	if !r.Has("alpha") {
		t.Fatal("expected Has('alpha') to be true")
	}
	if r.Has("missing") {
		t.Fatal("expected Has('missing') to be false")
	}
}

func TestNamesReturnsAllRegisteredNames(t *testing.T) {
	a := stubExecutor{name: "alpha"}
	b := stubExecutor{name: "beta"}
	c := stubExecutor{name: "gamma"}
	r := NewRegistry(a, b, c)

	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}

	sort.Strings(names)
	expected := []string{"alpha", "beta", "gamma"}
	for i, name := range names {
		if name != expected[i] {
			t.Fatalf("expected name %q at index %d, got %q", expected[i], i, name)
		}
	}
}
