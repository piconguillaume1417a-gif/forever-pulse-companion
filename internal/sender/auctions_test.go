package sender

import (
	"net/http"
	"testing"
	"time"
)

func TestAuctionRetryAfterNeverShortened(t *testing.T) {
	now := time.Unix(1791057600, 500000000)
	for _, c := range []struct {
		header string
		want   time.Duration
	}{{"1030", 1030 * time.Second}, {"7200", 2 * time.Hour}, {"", 30 * time.Minute}, {"nonsense", 30 * time.Minute}, {"0", time.Second}, {now.Add(2 * time.Hour).UTC().Format(http.TimeFormat), 2*time.Hour - 500*time.Millisecond}} {
		if got := auctionRetryAfter(c.header, now); got != c.want {
			t.Fatalf("H9 %q: %s expected %s", c.header, got, c.want)
		}
	}
}
