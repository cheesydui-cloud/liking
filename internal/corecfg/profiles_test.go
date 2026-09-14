package corecfg

import "testing"

func TestNormalizeCore(t *testing.T) {
	if NormalizeCore("sing-box") != CoreSingbox || NormalizeCore("Mita") != CoreMita || NormalizeCore("XRAY") != CoreXray {
		t.Fatalf("normalize")
	}
	if !KnownCore("singbox") || !KnownCore("mieru") || KnownCore("clash") {
		t.Fatalf("known")
	}
	if got := CoreNames(); len(got) != 3 || got[0] != CoreXray {
		t.Fatalf("names %v", got)
	}
}
