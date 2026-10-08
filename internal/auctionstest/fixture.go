// Package auctionstest builds synthetic CBOR and Lua, exclusively for tests.
package auctionstest

import (
	"encoding/binary"
	"fmt"
	"wowsync/internal/auctions"
	"wowsync/internal/testlua"
)

func head(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	case n < 65536:
		b := []byte{major<<5 | 25, 0, 0}
		binary.BigEndian.PutUint16(b[1:], uint16(n))
		return b
	default:
		b := make([]byte, 9)
		b[0] = major<<5 | 27
		binary.BigEndian.PutUint64(b[1:], n)
		return b
	}
}
func Encode(v any) []byte {
	switch x := v.(type) {
	case int:
		return Encode(int64(x))
	case int64:
		if x < 0 {
			return head(1, uint64(-1-x))
		}
		return head(0, uint64(x))
	case string:
		return append(head(3, uint64(len(x))), []byte(x)...)
	case []byte:
		return append(head(2, uint64(len(x))), x...)
	case []any:
		b := head(4, uint64(len(x)))
		for _, v := range x {
			b = append(b, Encode(v)...)
		}
		return b
	case auctions.Map:
		b := head(5, uint64(len(x)))
		for _, p := range x {
			b = append(b, Encode(p.Key)...)
			b = append(b, Encode(p.Value)...)
		}
		return b
	case nil:
		return []byte{0xf6}
	case bool:
		if x {
			return []byte{0xf5}
		}
		return []byte{0xf4}
	default:
		panic(fmt.Sprintf("unsupported test type %T", v))
	}
}
func LuaBytes(s []byte) string {
	out := "\""
	for _, b := range s {
		out += fmt.Sprintf("\\%03d", b)
	}
	return out + "\""
}
func File(m auctions.Map, version int) []byte {
	return []byte(fmt.Sprintf("AUCTIONATOR_PRICE_DATABASE = { __dbversion = %d, [\"PvP\"] = %s }\n", version, LuaBytes(Encode(m))))
}
func Prices(high, low, current int64) auctions.Map {
	obj := auctions.Map{{Key: []byte("h"), Value: auctions.Map{{Key: []byte("2467"), Value: high}}}, {Key: []byte("a"), Value: auctions.Map{{Key: int64(2467), Value: int64(0)}}}, {Key: []byte("l"), Value: []any{}}}
	if low > 0 {
		obj[2].Value = auctions.Map{{Key: int64(2467), Value: low}}
	}
	if current > 0 {
		obj = append(obj, auctions.Pair{Key: []byte("m"), Value: current})
	}
	return auctions.Map{{Key: []byte("version"), Value: int64(2)}, {Key: []byte("2589"), Value: obj}}
}

const Note = "1791044400|0|120|1791043800|18|1|90|PVP|PvP|4619|Alliance|1453|340"

func Forever(notes string) []byte {
	return []byte("ForeverPulseCensusDB = " + testlua.Serialise(testlua.M{"addon": testlua.M{"version": "3.11.0"}, "hdv": testlua.M{"v": 2, "depuis": 1790035200, "notes": notes}}) + "\n")
}
