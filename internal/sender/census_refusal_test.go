package sender

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCensusRefusalUsesExactErrorMeaning(t *testing.T) {
	for _, tc := range []struct {
		status int
		reason string
		want   Verdict
	}{
		{400, "invalid_body", Invalide}, {400, "invalid_source", SourceCoupee}, {400, "unknown", Panne},
		{400, "invalid_content_type", Incompatible}, {400, "invalid_encoding", Incompatible},
		{401, "token_required", JetonRefuse}, {401, "token_invalid", JetonRefuse}, {401, "token_revoked", JetonRefuse},
		{403, "token_revoked", JetonRefuse}, {403, "source_disabled", SourceCoupee}, {403, "unknown", Panne},
		{409, "unsupported_schema", Incompatible}, {409, "unknown", Panne},
		{413, "payload_too_large", TropGros}, {413, "unknown", Panne}, {401, "unknown", Panne},
	} {
		t.Run(fmt.Sprintf("%d_%s", tc.status, tc.reason), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": tc.reason})
			}))
			defer srv.Close()
			got := New(srv.URL, "synthetic-test").Post(context.Background(), "fpc_synthetic", deuxLots)
			if got.Verdict != tc.want {
				t.Fatalf("got verdict %d want %d", got.Verdict, tc.want)
			}
		})
	}
}
