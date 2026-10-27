package datapack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadsFileInsidePack(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompts", "x.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	text, ok, err := p.ReadText("prompts/x.md")
	if err != nil || !ok || text != "hello" {
		t.Fatalf("got %q %v %v", text, ok, err)
	}
	if _, ok, _ := p.ReadText("missing.md"); ok {
		t.Fatal("missing file reported as present")
	}
}

func TestRejectsPathsThatEscapeThePack(t *testing.T) {
	root := t.TempDir()
	p, _ := Open(root)
	for _, rel := range []string{"../outside.md", "prompts/../../outside.md", "../" + filepath.Base(root) + "-evil/x.md"} {
		if _, err := p.Resolve(rel); err == nil {
			t.Errorf("%q was allowed", rel)
		}
	}
}

func TestMissingRootFailsFast(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected an error")
	}
}
