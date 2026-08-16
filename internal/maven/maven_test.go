package maven

import "testing"

func TestParseAndPath(t *testing.T) {
	c, err := ParseCoordinate("com.example.modpacks:example-pack:1.0.1")
	if err != nil {
		t.Fatal(err)
	}
	want := "com/example/modpacks/example-pack/1.0.1/example-pack-1.0.1.mrpack"
	if got := c.Path("mrpack"); got != want {
		t.Fatalf("path: got %s want %s", got, want)
	}
	u := c.URL("https://maven.example.com", "mrpack")
	if u != "https://maven.example.com/"+want {
		t.Fatalf("url %s", u)
	}
}

func TestClassifier(t *testing.T) {
	c, err := ParseCoordinate("com.example.tools:example:1.0.0:linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	got := c.Path("bin")
	want := "com/example/tools/example/1.0.0/example-1.0.0-linux-amd64.bin"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNormalizeRepositories(t *testing.T) {
	got := NormalizeRepositories([]string{
		"https://a.example/",
		"",
		"https://b.example",
		"https://a.example",
	})
	if len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("%v", got)
	}
	if def := NormalizeRepositories(nil); len(def) != 0 {
		t.Fatalf("empty must stay empty, got %v", def)
	}
}

func TestNewClientBases(t *testing.T) {
	cl := NewClient("https://a.example", "https://b.example")
	if cl.Base() != "https://a.example" || len(cl.Bases) != 2 {
		t.Fatalf("%+v", cl)
	}
	cl = NewClient()
	if cl.Base() != "" || len(cl.Bases) != 0 {
		t.Fatalf("empty client should have no bases: %+v", cl)
	}
	if _, err := cl.FetchPack(Coordinate{Group: "g", Artifact: "a", Version: "1"}); err == nil {
		t.Fatal("expected ErrNoRepositories")
	}
}
