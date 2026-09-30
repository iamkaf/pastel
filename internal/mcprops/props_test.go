package mcprops

import "testing"

func TestUnescapeMOTDStripsColorCodesWithoutBreakingUTF8(t *testing.T) {
	got := unescapeMOTD(`§aBienvenue §lça va`)
	if got != "Bienvenue ça va" {
		t.Fatalf("got %q", got)
	}
}
