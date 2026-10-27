// Package datapack gives read-only access to the private data pack (profile, preferences, database).
// The pack lives outside the public repo; the code only knows its shape.
package datapack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Pack struct{ Root string }

// Open resolves root (relative to the working directory) and fails fast if it doesn't exist.
func Open(root string) (*Pack, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("data pack not found at %q; set DataPack:Path", abs)
	}
	return &Pack{Root: abs}, nil
}

// Resolve maps a pack-relative path to an absolute one, rejecting anything that escapes the root.
func (p *Pack) Resolve(rel string) (string, error) {
	full, err := filepath.Abs(filepath.Join(p.Root, rel))
	if err != nil {
		return "", err
	}
	if full != p.Root && !strings.HasPrefix(strings.ToLower(full), strings.ToLower(p.Root)+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the data pack", rel)
	}
	return full, nil
}

// ReadText returns the file's contents, or "" and ok=false if it doesn't exist.
func (p *Pack) ReadText(rel string) (string, bool, error) {
	path, err := p.Resolve(rel)
	if err != nil {
		return "", false, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(raw), true, nil
}
