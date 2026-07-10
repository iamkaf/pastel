package modrinth

import "testing"

func TestParsePageURL(t *testing.T) {
	slug, ver, ok := ParsePageURL("https://modrinth.com/modpack/aristea")
	if !ok || slug != "aristea" || ver != "" {
		t.Fatalf("%v %q %q", ok, slug, ver)
	}
	slug, ver, ok = ParsePageURL("https://www.modrinth.com/modpack/aristea/version/1.2.3")
	if !ok || slug != "aristea" || ver != "1.2.3" {
		t.Fatalf("%v %q %q", ok, slug, ver)
	}
	slug, ver, ok = ParsePageURL("https://modrinth.com/project/AABBCCDD")
	if !ok || slug != "AABBCCDD" {
		t.Fatalf("%v %q", ok, slug)
	}
	if _, _, ok := ParsePageURL("https://example.com/modpack/x"); ok {
		t.Fatal("expected reject")
	}
}

func TestParseRef(t *testing.T) {
	slug, ver, ok := ParseRef("modrinth:aristea")
	if !ok || slug != "aristea" || ver != "" {
		t.Fatalf("%v %q %q", ok, slug, ver)
	}
	slug, ver, ok = ParseRef("modrinth:aristea:1.0.0")
	if !ok || slug != "aristea" || ver != "1.0.0" {
		t.Fatalf("%v %q %q", ok, slug, ver)
	}
	if _, _, ok := ParseRef("aristea"); ok {
		t.Fatal("bare slug is not a ref")
	}
}

func TestLooksLikeSlug(t *testing.T) {
	if !LooksLikeSlug("aristea") || !LooksLikeSlug("my-pack_1") {
		t.Fatal("expected slug")
	}
	if LooksLikeSlug("com.iamkaf:x:1") || LooksLikeSlug("https://x") || LooksLikeSlug("a/b") || LooksLikeSlug("a@b") {
		t.Fatal("expected non-slug")
	}
}

func TestParseSlugVersion(t *testing.T) {
	slug, ver, ok := ParseSlugVersion("aristea:0.1.4")
	if !ok || slug != "aristea" || ver != "0.1.4" {
		t.Fatalf("%v %q %q", ok, slug, ver)
	}
	slug, ver, ok = ParseSlugVersion("aristea@0.1.4")
	if !ok || slug != "aristea" || ver != "0.1.4" {
		t.Fatalf("@ %v %q %q", ok, slug, ver)
	}
	// Maven coords are not slug:version
	if _, _, ok := ParseSlugVersion("com.iamkaf.modpacks:forever-world:1.1.0"); ok {
		t.Fatal("maven should not parse as slug:version")
	}
	if _, _, ok := ParseSlugVersion("modrinth:aristea:1.0"); ok {
		t.Fatal("modrinth: ref is ParseRef's job")
	}
	if _, _, ok := ParseSlugVersion("aristea"); ok {
		t.Fatal("bare slug is not slug:version")
	}
}

func TestSelectVersion(t *testing.T) {
	vs := []Version{
		{ID: "b", VersionNumber: "2.0.0", VersionType: "beta", Files: []File{{Filename: "p.mrpack", URL: "u", Primary: true}}},
		{ID: "a", VersionNumber: "1.0.0", VersionType: "release", Files: []File{{Filename: "p.mrpack", URL: "u", Primary: true}}},
	}
	// API returns newest first — first release with mrpack
	v, err := selectVersion(vs, "")
	if err != nil || v.ID != "a" {
		// wait - first in list is beta with mrpack, then we prefer release
		// selectVersion prefers release with mrpack over first
		if err != nil {
			t.Fatal(err)
		}
		if v.VersionType != "release" {
			t.Fatalf("want release, got %+v", v)
		}
	}
	v, err = selectVersion(vs, "2.0.0")
	if err != nil || v.ID != "b" {
		t.Fatalf("%v %+v", err, v)
	}
}
