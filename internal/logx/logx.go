// Package logx : journal tournant, 5 Mo × 3.
//
// Règle : aucun jeton, aucun nom de personnage. Uniquement des comptes, des
// identifiants de lot, des empreintes et des codes HTTP.
package logx

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const MaxSize = 5 << 20
const Keep = 3 // companion.log, companion.log.1, companion.log.2

type Logger struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
	Echo bool
}

func Open(path string) (*Logger, error) {
	l := &Logger{path: path}
	return l, l.open()
}

func (l *Logger) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, _ := f.Stat()
	l.f, l.size = f, st.Size()
	return nil
}

func (l *Logger) rotate() {
	l.f.Close()
	for i := Keep - 1; i >= 1; i-- {
		src := l.path
		if i > 1 {
			src = fmt.Sprintf("%s.%d", l.path, i-1)
		}
		_ = os.Rename(src, fmt.Sprintf("%s.%d", l.path, i))
	}
	_ = l.open()
}

func (l *Logger) Printf(format string, a ...any) {
	if l == nil {
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05 ") + fmt.Sprintf(format, a...) + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Echo {
		fmt.Fprint(os.Stderr, line)
	}
	if l.f == nil {
		return
	}
	if l.size+int64(len(line)) > MaxSize {
		l.rotate()
	}
	n, _ := l.f.WriteString(line)
	l.size += int64(n)
}

func (l *Logger) Path() string { return l.path }

func (l *Logger) Close() {
	if l != nil && l.f != nil {
		l.f.Close()
	}
}
