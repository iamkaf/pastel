package pack

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.1", "1.1.0", -1},
		{"1.1.0", "1.0.1", 1},
		{"1.1.0", "1.1.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q,%q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestMinecraft(t *testing.T) {
	m := &Manifest{Dependencies: map[string]string{"minecraft": "26.2", "fabric-loader": "0.19.3"}}
	if m.Minecraft() != "26.2" {
		t.Fatal(m.Minecraft())
	}
}
