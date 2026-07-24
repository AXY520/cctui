package update

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"v0.4.1":  "0.4.1",
		"V1.2.3":  "1.2.3",
		" 0.1.0 ": "0.1.0",
		"dev":     "dev",
	}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q)=%q want %q", input, got, want)
		}
	}
}

func TestCompareAndHasUpdate(t *testing.T) {
	if Compare("0.4.0", "0.4.1") >= 0 {
		t.Fatal("expected 0.4.0 < 0.4.1")
	}
	if Compare("0.4.1", "0.4.1") != 0 {
		t.Fatal("expected equal")
	}
	if Compare("0.5.0", "0.4.9") <= 0 {
		t.Fatal("expected 0.5.0 > 0.4.9")
	}
	if !HasUpdate("0.4.0", "0.4.1") {
		t.Fatal("expected update available")
	}
	if HasUpdate("0.4.1", "0.4.1") {
		t.Fatal("expected no update")
	}
	if !HasUpdate("dev", "0.4.1") {
		t.Fatal("dev should update to release")
	}
	if HasUpdate("0.4.2", "0.4.1") {
		t.Fatal("local newer should not update")
	}
}
