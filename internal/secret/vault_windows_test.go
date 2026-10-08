//go:build windows

package secret

import (
	"strings"
	"testing"
)

func TestActualIsolatedWindowsVault(t *testing.T) {
	v := ForContext(t.TempDir(), "http://127.0.0.1:3199", true)
	if v.target == Target || v.target == LegacyTarget {
		t.Fatal("unsafe test target")
	}
	t.Cleanup(func() { _ = remove(v.target); _ = v.DeleteConnection() })
	// Test-only credential: no hosted server accepts it; never read the user's vault target.
	token := "fpc_" + strings.Repeat("A", 43)
	if err := v.Set(token); err != nil {
		t.Fatal("isolated vault write failed")
	}
	if got, err := v.Get(); err != nil || got != token {
		t.Fatal("isolated vault round trip failed")
	}
	if err := v.WriteConnection(`{"id":"synthetic","completed":true}`); err != nil {
		t.Fatal("connection vault write failed")
	}
	if got, err := v.ReadConnection(); err != nil || !strings.Contains(got, "synthetic") {
		t.Fatal("connection vault round trip failed")
	}
	if err := v.DeleteConnection(); err != nil {
		t.Fatal("connection cleanup failed")
	}
	if _, err := v.ReadConnection(); err == nil {
		t.Fatal("connection still stored")
	}
}
