package buildinfo

import "testing"

func TestUserAgent(t *testing.T) {
	if got := UserAgent(); got != "Pastel/0.1.0-dev (+https://kaf.sh/pastel)" {
		t.Fatalf("default UserAgent = %q", got)
	}
}

func TestUserAgentUsesVersion(t *testing.T) {
	prev := Version
	t.Cleanup(func() { Version = prev })
	Version = "0.1.5"
	if got := UserAgent(); got != "Pastel/0.1.5 (+https://kaf.sh/pastel)" {
		t.Fatalf("UserAgent = %q", got)
	}
	Version = "  "
	if got := UserAgent(); got != "Pastel/dev (+https://kaf.sh/pastel)" {
		t.Fatalf("empty version UserAgent = %q", got)
	}
}
