// Package auctions implements CONTRAT-HDV-v1 revision 2. Auctionator code is
// never used. This decoder is written from RFC 8949 section 3 and appendix A:
// https://www.rfc-editor.org/rfc/rfc8949.html
package auctions

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

const MaxDepth = 8
const MaxElements = 200000
const MaxString = 8 << 20

type Pair struct{ Key, Value any }
type Map []Pair

// SavedVariables observed in the black-box check uses CBOR byte strings for
// field names. No Auctionator code is consulted. Text keys also remain valid.
func keyText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	}
	return "", false
}

func (m Map) Get(k string) any {
	for _, p := range m {
		if name, ok := keyText(p.Key); ok && name == k {
			return p.Value
		}
	}
	return nil
}

type decoder struct {
	data          []byte
	pos, elements int
}

var errCBOR = errors.New("CBOR non pris en charge ou illisible")

// DecodeCBOR accepts only the bounded, definite-length subset in L3.
func DecodeCBOR(data []byte) (any, error) {
	d := decoder{data: data}
	v, err := d.value(0)
	if err != nil || d.pos != len(data) {
		return nil, errCBOR
	}
	return v, nil
}
func (d *decoder) take(n uint64) ([]byte, error) {
	if n > uint64(len(d.data)-d.pos) {
		return nil, errCBOR
	}
	b := d.data[d.pos : d.pos+int(n)]
	d.pos += int(n)
	return b, nil
}
func (d *decoder) value(depth int) (any, error) {
	d.elements++
	if depth > MaxDepth || d.elements > MaxElements {
		return nil, errCBOR
	}
	b, err := d.take(1)
	if err != nil {
		return nil, err
	}
	major, ai := b[0]>>5, b[0]&31
	if major == 7 {
		switch ai {
		case 20:
			return false, nil
		case 21:
			return true, nil
		case 22:
			return nil, nil
		}
		return nil, errCBOR
	}
	var n uint64
	switch {
	case ai < 24:
		n = uint64(ai)
	case ai <= 27:
		b, err = d.take(uint64(1) << (ai - 24))
		if err != nil {
			return nil, err
		}
		switch len(b) {
		case 1:
			n = uint64(b[0])
		case 2:
			n = uint64(binary.BigEndian.Uint16(b))
		case 4:
			n = uint64(binary.BigEndian.Uint32(b))
		case 8:
			n = binary.BigEndian.Uint64(b)
		}
	default:
		return nil, errCBOR
	}
	switch major {
	case 0:
		if n > math.MaxInt64 {
			return nil, errCBOR
		}
		return int64(n), nil
	case 1:
		if n > math.MaxInt64 {
			return nil, errCBOR
		}
		return -1 - int64(n), nil
	case 2, 3:
		if n > MaxString {
			return nil, errCBOR
		}
		b, err = d.take(n)
		if err != nil {
			return nil, err
		}
		if major == 3 {
			if !utf8.Valid(b) {
				return nil, errCBOR
			}
			return string(b), nil
		}
		return append([]byte(nil), b...), nil
	case 4:
		if n > uint64(MaxElements-d.elements) {
			return nil, errCBOR
		}
		a := make([]any, 0, int(n))
		for i := uint64(0); i < n; i++ {
			v, e := d.value(depth + 1)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		return a, nil
	case 5:
		if n > uint64(MaxElements-d.elements)/2 {
			return nil, errCBOR
		}
		m := make(Map, 0, int(n))
		seen := map[any]bool{}
		for i := uint64(0); i < n; i++ {
			k, e := d.value(depth + 1)
			if e != nil {
				return nil, e
			}
			// Container keys and byte/text aliases for the same field are unexpected.
			var key any
			switch x := k.(type) {
			case string:
				key = struct{ Text string }{x}
			case []byte:
				key = struct{ Text string }{string(x)}
			case int64, bool, nil:
				key = x
			default:
				return nil, errCBOR
			}
			if seen[key] {
				return nil, errCBOR
			}
			seen[key] = true
			v, e := d.value(depth + 1)
			if e != nil {
				return nil, e
			}
			m = append(m, Pair{k, v})
		}
		return m, nil
	default:
		return nil, errCBOR
	}
}
