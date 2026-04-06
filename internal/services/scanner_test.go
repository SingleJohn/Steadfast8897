package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseNfoReadsStudioTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.nfo")
	content := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Example</title>
  <studio>Netflix</studio>
</movie>`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write nfo: %v", err)
	}

	nfo := ParseNfo(path)
	if nfo == nil {
		t.Fatal("expected nfo result")
	}
	if nfo.Studio == nil || *nfo.Studio != "Netflix" {
		t.Fatalf("expected studio Netflix, got %#v", nfo.Studio)
	}
}
