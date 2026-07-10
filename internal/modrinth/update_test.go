package modrinth

import "testing"

func TestCompareVersionLabels(t *testing.T) {
	if compareVersionLabels("1.7.2", "1.7.3") >= 0 {
		t.Fatal("1.7.2 < 1.7.3")
	}
	if compareVersionLabels("1.7.3", "1.7.3") != 0 {
		t.Fatal("equal")
	}
	if compareVersionLabels("2.0.0", "1.9.9") <= 0 {
		t.Fatal("2 > 1.9.9")
	}
}

func TestPin(t *testing.T) {
	if Pin("aristea", "") != "modrinth:aristea" {
		t.Fatal(Pin("aristea", ""))
	}
	if Pin("aristea", "1.2.3") != "modrinth:aristea:1.2.3" {
		t.Fatal(Pin("aristea", "1.2.3"))
	}
}
