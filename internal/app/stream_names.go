package app

import (
	"io"
	"sort"
	"strings"

	"wowsync/internal/lua"
)

type nameBlock struct {
	end  int
	span lua.Span
}

type nameIndex struct {
	src     io.ReaderAt
	blocks  []nameBlock
	hot     map[int][]string
	order   [32]int
	next    int
	failure error
}

func openNames(src io.ReaderAt, spans map[int64]item) (*nameIndex, error) {
	n := &nameIndex{src: src, hot: map[int][]string{}}
	for j := range n.order {
		n.order[j] = -1
	}
	end := 0
	for i := int64(1); ; i++ {
		it, ok := spans[i]
		if !ok {
			break
		}
		v, err := it.span.Value(src, 2<<20)
		if err != nil {
			return nil, err
		}
		for s := range strings.SplitSeq(lua.Str(v), "\n") {
			if s != "" {
				end++
			}
		}
		if len(n.blocks) == 0 || end > n.blocks[len(n.blocks)-1].end {
			n.blocks = append(n.blocks, nameBlock{end: end, span: it.span})
		}
	}
	return n, nil
}

// Dictionnaire commun: a bounded cache of decoded blocks, indexed by their
// cumulative row counts. Old blocks can be read back from the same file handle.
func (n *nameIndex) lookup(index int) (string, bool) {
	if index < 1 {
		return "", false
	}
	i := sort.Search(len(n.blocks), func(i int) bool { return n.blocks[i].end >= index })
	if i == len(n.blocks) {
		return "", false
	}
	block, ok := n.hot[i]
	if !ok {
		v, err := n.blocks[i].span.Value(n.src, 2<<20)
		if err != nil {
			n.failure = err
			return "", false
		}
		for s := range strings.SplitSeq(lua.Str(v), "\n") {
			if s != "" {
				block = append(block, s)
			}
		}
		if n.order[n.next] >= 0 {
			delete(n.hot, n.order[n.next])
		}
		n.order[n.next] = i
		n.next = (n.next + 1) % len(n.order)
		n.hot[i] = block
	}
	start := 0
	if i > 0 {
		start = n.blocks[i-1].end
	}
	offset := index - 1 - start
	if offset < 0 || offset >= len(block) {
		return "", false
	}
	return block[offset], true
}
