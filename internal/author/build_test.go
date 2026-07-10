package author

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRejectsUnsafeOutputNames(t *testing.T) {
	t.Parallel()
	_, err := Build(BuildOptions{
		Mrpack:  "unused",
		Slug:    "../escape",
		Version: "1.0.0",
		OutDir:  t.TempDir(),
	})
	if err == nil {
		t.Fatal("Build unexpectedly accepted an unsafe slug")
	}
}

func TestMinimalPOMEscapesXML(t *testing.T) {
	t.Parallel()
	pom := minimalPOM("com.example", "pack", "1.0.0", `A & B <Pack>`)
	if strings.Contains(pom, "A & B <Pack>") || !strings.Contains(pom, "A &amp; B &lt;Pack&gt;") {
		t.Fatalf("POM name was not XML escaped: %s", pom)
	}
}

func TestPublishRejectsArtifactOutsideBuildDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	meta := `{"group":"com.example","artifact":"pack","version":"1.0.0","packFile":"../outside.mrpack","pom":"pack-1.0.0.pom"}`
	if err := os.WriteFile(filepath.Join(dir, "publish.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(PublishOptions{Dir: dir, PublishURL: "https://example.invalid", DryRun: true}); err == nil {
		t.Fatal("Publish unexpectedly accepted an outside artifact")
	}
}
