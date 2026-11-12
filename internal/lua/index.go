package lua

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
)

// Span locates a value in a SavedVariables file. The scanner validates the
// whole stream without retaining table values. Callers choose which offsets
// to retain, then read just those values with ReaderAt.
type Span struct{ Start, End int64 }
type Key struct {
	Name     string
	Index    int64
	Explicit bool
}

func (s Span) Value(src io.ReaderAt, max int64) (any, error) {
	if s.Start < 0 || s.End < s.Start || s.End-s.Start > max {
		return nil, errors.New("SavedVariables value exceeds its field limit")
	}
	b := make([]byte, 2+int(s.End-s.Start))
	copy(b, "v=")
	if _, err := src.ReadAt(b[2:], s.Start); err != nil {
		return nil, err
	}
	v, err := Parse(b, nil)
	return v["v"], err
}

type scanner struct {
	r     *bufio.Reader
	at    int64
	line  int
	ctx   context.Context
	path  [MaxDepth + 1]Key
	visit func([]Key, Span) error
}

func (s *scanner) fail(msg string) error { return &ParseError{Line: s.line, Msg: msg} }
func (s *scanner) peek() byte {
	b, _ := s.r.Peek(1)
	if len(b) == 0 {
		return 0
	}
	return b[0]
}
func (s *scanner) next() (byte, error) {
	if s.at%65536 == 0 {
		if err := s.ctx.Err(); err != nil {
			return 0, err
		}
	}
	b, err := s.r.ReadByte()
	if err == nil {
		s.at++
		if b == '\n' {
			s.line++
		}
	}
	return b, err
}
func (s *scanner) eat(n int) error {
	for i := 0; i < n; i++ {
		if _, err := s.next(); err != nil {
			return err
		}
	}
	return nil
}
func (s *scanner) blanks() error {
	for {
		switch s.peek() {
		case ' ', '\t', '\r', '\n':
			if err := s.eat(1); err != nil {
				return err
			}
		case '-':
			p, _ := s.r.Peek(4)
			if len(p) < 2 || p[1] != '-' {
				return nil
			}
			long := len(p) == 4 && p[2] == '[' && p[3] == '['
			if !long {
				if err := s.eat(2); err != nil {
					return err
				}
				for {
					c, e := s.next()
					if e == io.EOF {
						return nil
					}
					if e != nil {
						return e
					}
					if c == '\n' {
						break
					}
				}
			} else {
				if err := s.eat(4); err != nil {
					return err
				}
				for {
					c, e := s.next()
					if e == io.EOF {
						return s.fail("unterminated comment")
					}
					if e != nil {
						return e
					}
					if c == ']' && s.peek() == ']' {
						if err := s.eat(1); err != nil {
							return err
						}
						break
					}
				}
			}
		default:
			return nil
		}
	}
}
func (s *scanner) want(b byte) error {
	if err := s.blanks(); err != nil {
		return err
	}
	if s.peek() != b {
		return s.fail("unexpected token")
	}
	return s.eat(1)
}
func (s *scanner) word() (string, error) {
	var buf [256]byte
	i := 0
	for isName(s.peek()) {
		if i == len(buf) {
			return "", s.fail("identifier too long")
		}
		b, e := s.next()
		if e != nil {
			return "", e
		}
		buf[i] = b
		i++
	}
	return string(buf[:i]), nil
}
func (s *scanner) quoted(keep bool) (string, error) {
	q, err := s.next()
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for {
		c, err := s.next()
		if err == io.EOF {
			return "", s.fail("unterminated string")
		}
		if err != nil {
			return "", err
		}
		if c == q {
			return out.String(), nil
		}
		if c == '\\' {
			c, err = s.next()
			if err != nil {
				return "", s.fail("truncated escape")
			}
			if isDigit(c) {
				n := int(c - '0')
				for i := 0; i < 2 && isDigit(s.peek()); i++ {
					b, e := s.next()
					if e != nil {
						return "", e
					}
					n = n*10 + int(b-'0')
				}
				if n > 255 {
					return "", s.fail("invalid decimal escape")
				}
				c = byte(n)
			} else if b, ok := escapes[c]; ok {
				c = b
			}
		}
		if keep {
			if out.Len() >= 8192 {
				return "", s.fail("table key too long")
			}
			out.WriteByte(c)
		}
	}
}
func (s *scanner) scalar(keep bool) (any, error) {
	c := s.peek()
	if c == '"' || c == '\'' {
		return s.quoted(keep)
	}
	if isNameStart(c) {
		w, e := s.word()
		if e != nil {
			return nil, e
		}
		switch w {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "nil":
			return nil, nil
		}
		return nil, s.fail("unexpected identifier")
	}
	var buf [256]byte
	i := 0
	for c = s.peek(); isName(c) || c == '-' || c == '+' || c == '.'; c = s.peek() {
		if c == '-' && i > 0 {
			p, _ := s.r.Peek(2)
			if len(p) == 2 && p[1] == '-' {
				break
			}
		}
		if i == len(buf) {
			return nil, s.fail("number too long")
		}
		b, e := s.next()
		if e != nil {
			return nil, e
		}
		buf[i] = b
		i++
	}
	r := reader{s: buf[:i]}
	n, ok := r.number()
	if !ok || r.i != i {
		return nil, s.fail("unexpected value")
	}
	return n, nil
}
func (s *scanner) value(depth int, tracked bool) error {
	if err := s.blanks(); err != nil {
		return err
	}
	start := s.at
	if s.peek() == '{' {
		if depth == MaxDepth {
			return s.fail("tables nested too deeply")
		}
		if err := s.eat(1); err != nil {
			return err
		}
		var i int64
		for {
			if err := s.blanks(); err != nil {
				return err
			}
			c := s.peek()
			if c == '}' {
				if err := s.eat(1); err != nil {
					return err
				}
				break
			}
			key := Key{}
			if c == '[' {
				if err := s.eat(1); err != nil {
					return err
				}
				if err := s.blanks(); err != nil {
					return err
				}
				k, e := s.scalar(tracked)
				if e != nil {
					return e
				}
				key.Explicit = true
				switch x := k.(type) {
				case string:
					key.Name = x
				case Number:
					if x.IsInt {
						key.Index = x.I
					} else if x.F > -1<<62 && x.F < 1<<62 && float64(int64(x.F)) == x.F {
						key.Index = int64(x.F)
					}
				case nil:
					return s.fail("nil table key")
				}
				if err := s.want(']'); err != nil {
					return err
				}
				if err := s.want('='); err != nil {
					return err
				}
			} else if isNameStart(c) {
				at := s.at
				w, e := s.word()
				if e != nil {
					return e
				}
				if err := s.blanks(); err != nil {
					return err
				}
				if s.peek() == '=' {
					key.Name = w
					key.Explicit = true
					if err := s.eat(1); err != nil {
						return err
					}
				} else {
					if w != "true" && w != "false" && w != "nil" {
						return s.fail("unexpected identifier")
					}
					i++
					if tracked {
						s.path[depth+1] = Key{Index: i}
						if err := s.visit(s.path[:depth+2], Span{at, at + int64(len(w))}); err != nil {
							return err
						}
					}
					if c := s.peek(); c == ',' || c == ';' {
						if err := s.eat(1); err != nil {
							return err
						}
					}
					continue
				}
			} else {
				i++
				key.Index = i
			}
			if tracked {
				s.path[depth+1] = key
			}
			if err := s.value(depth+1, tracked); err != nil {
				return err
			}
			if err := s.blanks(); err != nil {
				return err
			}
			if c := s.peek(); c == ',' || c == ';' {
				if err := s.eat(1); err != nil {
					return err
				}
			}
		}
	} else if _, err := s.scalar(false); err != nil {
		return err
	}
	if tracked {
		return s.visit(s.path[:depth+1], Span{start, s.at})
	}
	return nil
}

// Index parses assignments only. visit is called bottom up; the path slice is
// borrowed for the duration of the call. Unwanted roots do not allocate keys.
func Index(ctx context.Context, src io.Reader, keep map[string]bool, visit func([]Key, Span) error) error {
	s := &scanner{r: bufio.NewReaderSize(src, 128<<10), line: 1, ctx: ctx, visit: visit}
	if b, _ := s.r.Peek(3); string(b) == "\xef\xbb\xbf" {
		if e := s.eat(3); e != nil {
			return e
		}
	}
	for {
		if err := s.blanks(); err != nil {
			return err
		}
		p, e := s.r.Peek(1)
		if e == io.EOF {
			return ctx.Err()
		}
		if e != nil {
			return e
		}
		if !isNameStart(p[0]) {
			return s.fail("expected assignment")
		}
		w, e := s.word()
		if e != nil {
			return e
		}
		if e = s.want('='); e != nil {
			return e
		}
		s.path[0] = Key{Name: w, Explicit: true}
		if e = s.value(0, keep[w]); e != nil {
			return e
		}
		if e = s.blanks(); e != nil {
			return e
		}
		if s.peek() == ';' {
			if e = s.eat(1); e != nil {
				return e
			}
		}
	}
}
