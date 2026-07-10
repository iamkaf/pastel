package pack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildJavaArgsFabricJar(t *testing.T) {
	dir := t.TempDir()
	jar := "fabric-server-mc.26.1.2-loader.0.19.2-launcher.1.1.1.jar"
	if err := os.WriteFile(filepath.Join(dir, jar), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Launch: &Launch{Kind: "fabric", Jar: jar},
	}
	args, err := m.BuildJavaArgs(dir, "4G")
	if err != nil {
		t.Fatal(err)
	}
	// -Xmx4G … -jar <path> nogui
	if args[0] != "-Xmx4G" {
		t.Fatalf("args[0]=%s", args[0])
	}
	foundJar, foundNogui := false, false
	for i, a := range args {
		if a == "-jar" && i+1 < len(args) {
			foundJar = true
		}
		if a == "nogui" {
			foundNogui = true
		}
	}
	if !foundJar || !foundNogui {
		t.Fatalf("args=%v", args)
	}
}

func TestBuildJavaArgsNeoForgeArgsFile(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join("libraries", "net", "neoforged", "neoforge", "21.0.0", "unix_args.txt")
	full := filepath.Join(dir, argsPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("-p libraries\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	jvm := filepath.Join(dir, "user_jvm_args.txt")
	if err := os.WriteFile(jvm, []byte("# jvm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Launch: &Launch{
			Kind:        "neoforge",
			JVMArgsFile: "user_jvm_args.txt",
			ArgsFile:    argsPath,
		},
	}
	args, err := m.BuildJavaArgs(dir, "6G")
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	if args[0] != "-Xmx6G" {
		t.Fatalf("want -Xmx first: %v", args)
	}
	if !containsPrefixed(args, "@") {
		t.Fatalf("expected @args files: %v", args)
	}
	if args[len(args)-1] != "nogui" {
		t.Fatalf("want nogui last: %v", args)
	}
}

func containsPrefixed(args []string, prefix string) bool {
	for _, a := range args {
		if len(a) >= len(prefix) && a[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func TestIsManagedRootJar(t *testing.T) {
	if !IsManagedRootJar("fabric-server-mc.26.1.2-loader.0.19.2-launcher.1.1.1.jar") {
		t.Fatal("fabric")
	}
	if !IsManagedRootJar("neoforge-21.0.0-universal.jar") {
		t.Fatal("neoforge")
	}
	if !IsManagedRootJar("server.jar") {
		t.Fatal("server.jar")
	}
	if IsManagedRootJar("mods/foo.jar") {
		t.Fatal("mods jar should not match as root name alone — path has slash")
	}
	// basename only
	if IsManagedRootJar("lithium.jar") {
		t.Fatal("random jar")
	}
}

func TestLoaderKindFromDeps(t *testing.T) {
	m := &Manifest{Dependencies: map[string]string{"neoforge": "21.0.0", "minecraft": "1.21.1"}}
	if m.LoaderKind() != "NeoForge" || m.ResolvedKind() != "neoforge" {
		t.Fatalf("%s %s", m.LoaderKind(), m.ResolvedKind())
	}
}
