package secret

import "testing"

func TestVaultScopePreservesProductionAndIsolatesTests(t *testing.T) {
	prod := ForContext("original", "https://forever-pulse.com", false)
	if prod.target != Target {
		t.Fatal("legacy credential must be preserved")
	}
	isolated := ForContext("candidate", "https://forever-pulse.com", true)
	if isolated.target == Target || isolated.target == ForContext("other", "https://forever-pulse.com", true).target {
		t.Fatal("directory isolation")
	}
	if isolated.target == ForContext("candidate", "https://staging.example.invalid", true).target {
		t.Fatal("site isolation")
	}
}
