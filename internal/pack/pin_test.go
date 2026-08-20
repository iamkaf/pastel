package pack

import (
	"testing"

	"github.com/iamkaf/pastel/internal/modrinth"
)

func TestClassifyPin(t *testing.T) {
	tests := []struct {
		raw  string
		want PinKind
	}{
		{"https://example.com/pack.mrpack", PinURL},
		{"http://example.com/pack.mrpack", PinURL},
		{"HTTPS://example.com/pack.mrpack", PinURL},
		{"https://modrinth.com/modpack/aristea", PinModrinth},
		{"https://modrinth.com/modpack/aristea/version/1.2.3", PinModrinth},
		{"https://modrinth.com/project/AABBCCDD", PinModrinth},
		{"file:///tmp/pack.mrpack", PinFile},
		{"FILE:pack.mrpack", PinFile},
		{"modrinth:aristea", PinModrinth},
		{"modrinth:aristea:1.2.3", PinModrinth},
		{"aristea:0.1.4", PinModrinth},
		{"aristea@0.1.4", PinModrinth},
		{"aristea", PinModrinth},
		{"com.example.modpacks:example-pack:1.2.0", PinMaven},
		{"com.example.modpacks:example-pack:1.2.0:shaded", PinMaven},
		{"./packs/local.mrpack", PinPath},
		{"/abs/path/pack.mrpack", PinPath},
		{"C:\\servers\\pack.mrpack", PinPath},
		{"", PinPath},
		{"   ", PinPath},
	}
	for _, tt := range tests {
		if got := ClassifyPin(tt.raw); got != tt.want {
			t.Errorf("ClassifyPin(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// Maven coordinates and slug shorthands must stay disjoint so the
// ClassifyPin ordering can never misroute a pin.
func TestMavenAndSlugShorthandAreDisjoint(t *testing.T) {
	mavens := []string{
		"com.example:pack:1.0.0",
		"com.example:pack:1.0.0:shaded",
		"a.b:c:d",
	}
	for _, s := range mavens {
		slug, ver, ok := modrinth.ParseSlugVersion(s)
		if ok {
			t.Errorf("ParseSlugVersion(%q) = (%q, %q), want no match", s, slug, ver)
		}
	}
	slugs := []string{"aristea:0.1.4", "aristea@0.1.4"}
	for _, s := range slugs {
		if IsMavenCoordinate(s) {
			t.Errorf("IsMavenCoordinate(%q) = true, want false", s)
		}
	}
}

func TestIsRelativePathPin(t *testing.T) {
	if !IsRelativePathPin("./packs/local.mrpack") {
		t.Error("relative path should classify as path pin")
	}
	if IsRelativePathPin("") {
		t.Error("empty pin must not be a relative path")
	}
	if IsRelativePathPin("modrinth:aristea") {
		t.Error("scheme pins must not be relative paths")
	}
	if IsRelativePathPin("com.example:pack:1.0.0") {
		t.Error("maven coordinates must not be relative paths")
	}
}
