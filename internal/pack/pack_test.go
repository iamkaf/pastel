package pack

import "testing"

func TestValidate(t *testing.T) {
	m := &Manifest{
		Name:    "Example Pack",
		Version: "1.0.1",
		Dependencies: map[string]string{
			"minecraft":     "26.1.2",
			"fabric-loader": "0.19.2",
		},
		Files: []File{{
			Path:      "mods/example.jar",
			Hashes:    map[string]string{"sha512": "abc"},
			Downloads: []string{"https://cdn.example.com/example.jar"},
		}},
		Launch: &Launch{Jar: "fabric-server-mc.26.1.2-loader.0.19.2-launcher.1.1.1.jar"},
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	algo, hex, ok := m.Files[0].PreferredHash()
	if !ok || algo != "sha512" || hex != "abc" {
		t.Fatalf("hash %s %s %v", algo, hex, ok)
	}
}

func TestValidateAllowsWorldPath(t *testing.T) {
	m := &Manifest{
		Name:    "x",
		Version: "1",
		Files: []File{{
			Path:      "world/foo",
			Hashes:    map[string]string{"sha256": "aa"},
			Downloads: []string{"https://example.com/foo"},
		}},
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if m.Files[0].Path != "world/foo" {
		t.Fatal("expected path stored")
	}
}

func TestValidateRejectsDotDot(t *testing.T) {
	m := &Manifest{
		Name:    "x",
		Version: "1",
		Files: []File{{
			Path:      "mods/../secret",
			Hashes:    map[string]string{"sha256": "aa"},
			Downloads: []string{"https://example.com/foo"},
		}},
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for .. in path")
	}
}

func TestValidateRequiresDownloads(t *testing.T) {
	m := &Manifest{
		Name:    "x",
		Version: "1",
		Files: []File{{
			Path:   "mods/example.jar",
			Hashes: map[string]string{"sha256": "aa"},
		}},
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for missing downloads")
	}
}
