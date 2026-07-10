package jre

import "testing"

func TestRequireMajor(t *testing.T) {
	cases := map[string]int{
		"":       25,
		"26.1":   25,
		"26.1.2": 25,
		"26.2":   25,
		"27.0":   25,
		"1.20.4": 17,
		"1.20.5": 21,
		"1.21":   21,
		"1.16.5": 8,
		"1.17":   17,
		"1.18.2": 17,
	}
	for mc, want := range cases {
		if got := RequireMajor(mc); got != want {
			t.Errorf("RequireMajor(%q)=%d want %d", mc, got, want)
		}
	}
}
