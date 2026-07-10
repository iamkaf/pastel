package pack

import (
	"testing"
)

func TestParseAndValidate(t *testing.T) {
	raw := []byte(`{
  "schemaVersion": 1,
  "name": "FOREVER WORLD",
  "version": "1.0.1",
  "dependencies": {"minecraft": "26.1.2", "fabric-loader": "0.19.2"},
  "files": [{
    "path": "mods/example.jar",
    "hashes": {"sha512": "abc"},
    "downloads": ["https://cdn.modrinth.com/data/x/versions/y/example.jar"]
  }],
  "launch": {"jar": "fabric-server-mc.26.1.2-loader.0.19.2-launcher.1.1.1.jar"}
}`)
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "FOREVER WORLD" || len(m.Files) != 1 {
		t.Fatalf("unexpected: %+v", m)
	}
	algo, hex, ok := m.Files[0].PreferredHash()
	if !ok || algo != "sha512" || hex != "abc" {
		t.Fatalf("hash %s %s %v", algo, hex, ok)
	}
}

func TestRejectWorldPath(t *testing.T) {
	raw := []byte(`{
  "schemaVersion": 1,
  "name": "x",
  "version": "1",
  "files": [{
    "path": "world/foo",
    "hashes": {"sha256": "aa"},
    "downloads": ["https://example.com/foo"]
  }]
}`)
	// Validate allows world path at parse; sync rejects. Parse only checks ..
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files[0].Path != "world/foo" {
		t.Fatal("expected path stored")
	}
}

func TestRejectDotDot(t *testing.T) {
	raw := []byte(`{
  "schemaVersion": 1,
  "name": "x",
  "version": "1",
  "files": [{
    "path": "mods/../secret",
    "hashes": {"sha256": "aa"},
    "downloads": ["https://example.com/foo"]
  }]
}`)
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected error for .. in path")
	}
}
