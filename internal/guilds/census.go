package guilds

import (
	"crypto/sha256"
	"fmt"
)

const Method = "guild_membership"
const Chunk = 250

// BatchID is independent of row order and the source file. The ID of the
// canonical complete snapshot already includes its observation time and scope.
func BatchID(snapshot string, part int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("forever-pulse:guild:%s:%d", snapshot, part)))
	sum[6] = (sum[6] & 15) | 64
	sum[8] = (sum[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
