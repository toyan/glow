package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewModelLoadsDocumentBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte("# Hello\n\nSome text here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newModel(Config{Path: path}, "").(model)
	if m.fatalErr != nil {
		t.Fatalf("expected no error, got %v", m.fatalErr)
	}
	if !strings.Contains(m.pager.currentDocument.Body, "Some text") {
		t.Errorf("expected document body to be loaded, got %q", m.pager.currentDocument.Body)
	}
}
