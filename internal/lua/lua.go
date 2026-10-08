// Package lua lit le sous-ensemble de Lua qu'écrit WoW dans les SavedVariables.
//
// Portage de forever-pulse-roadmap/outils/lua_tables.py. Le jeu écrit des tables
// nues : `Var = { ["clé"] = valeur, ... }`. Pas de code, pas d'appel, pas
// d'expression : on analyse sans interpréteur. Ce qui n'est pas compris lève une
// erreur — un fichier qu'on ne comprend pas ne doit jamais être envoyé à moitié.
//
// Écarts voulus avec la référence Python, sans effet sur les fichiers de l'addon :
//   - `\ddd` produit l'octet ddd (règle Lua), là où Python produit le caractère
//     Unicode ddd ; les deux coïncident en ASCII, seul cas écrit par le jeu ;
//   - les commentaires longs `--[[ … ]]` sont reconnus.
//
// La profondeur d'imbrication est bornée (MaxDepth) : aucun fichier, même forgé,
// ne peut faire déborder la pile.
package lua

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// MaxDepth borne l'imbrication des tables. Le fichier de l'addon en compte 5.
const MaxDepth = 64

// Number garde la distinction entier / décimal de la référence Python.
type Number struct {
	I     int64
	F     float64
	IsInt bool
}

func (n Number) Float() float64 {
	if n.IsInt {
		return float64(n.I)
	}
	return n.F
}

// Table : clés chaînes, clés entières (explicites ou positionnelles) et autres.
// Une valeur `nil` écrite explicitement est conservée (nil dans la carte), comme
// le fait la référence Python ; Get la rend comme absente.
type Table struct {
	Str   map[string]any
	Int   map[int64]any
	Other map[string]any // clés décimales ou booléennes, rendues en texte
	Order []string       // ordre d'apparition des clés chaînes
}

func newTable() *Table {
	return &Table{Str: map[string]any{}, Int: map[int64]any{}}
}

// Get renvoie la valeur d'une clé chaîne (nil si absente ou nil).
func (t *Table) Get(k string) any {
	if t == nil {
		return nil
	}
	return t.Str[k]
}

// Has dit si la clé chaîne est présente avec une valeur non nil.
func (t *Table) Has(k string) bool {
	if t == nil {
		return false
	}
	v, ok := t.Str[k]
	return ok && v != nil
}

// List : les clés 1..n, dans l'ordre, jusqu'au premier trou (liste() en Python).
func (t *Table) List() []any {
	if t == nil {
		return nil
	}
	var out []any
	for i := int64(1); ; i++ {
		v, ok := t.Int[i]
		if !ok {
			return out
		}
		out = append(out, v)
	}
}

// ParseError porte la ligne de l'erreur.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("ligne %d : %s", e.Line, e.Msg) }

type reader struct {
	s     []byte
	i     int
	depth int
}

func (r *reader) fail(format string, a ...any) error {
	line := bytes.Count(r.s[:min(r.i, len(r.s))], []byte("\n")) + 1
	return &ParseError{Line: line, Msg: fmt.Sprintf(format, a...)}
}

func (r *reader) blanks() {
	for r.i < len(r.s) {
		c := r.s[r.i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			r.i++
		case c == '-' && r.i+1 < len(r.s) && r.s[r.i+1] == '-':
			if r.i+3 < len(r.s) && r.s[r.i+2] == '[' && r.s[r.i+3] == '[' {
				end := bytes.Index(r.s[r.i+4:], []byte("]]"))
				if end < 0 {
					r.i = len(r.s)
					return
				}
				r.i += 4 + end + 2
				continue
			}
			end := bytes.IndexByte(r.s[r.i:], '\n')
			if end < 0 {
				r.i = len(r.s)
			} else {
				r.i += end
			}
		default:
			return
		}
	}
}

func (r *reader) expect(c byte) error {
	r.blanks()
	if r.i >= len(r.s) || r.s[r.i] != c {
		return r.fail("« %c » attendu", c)
	}
	r.i++
	return nil
}

var escapes = map[byte]byte{'n': '\n', 'r': '\r', 't': '\t', '"': '"', '\\': '\\', '\'': '\'', 'a': 7, 'b': 8, 'f': 12, 'v': 11}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isName(c byte) bool { return isNameStart(c) || isDigit(c) }

// str lit une chaîne. En mode skip, rien n'est alloué.
func (r *reader) str(skip bool) (string, error) {
	q := r.s[r.i]
	r.i++
	var out []byte
	start := r.i
	simple := true // tant qu'aucun échappement : on découpe sans copier
	for r.i < len(r.s) {
		c := r.s[r.i]
		if c == q {
			var v string
			if !skip {
				if simple {
					v = string(r.s[start:r.i])
				} else {
					v = string(out)
				}
			}
			r.i++
			return v, nil
		}
		if c == '\\' {
			if simple && !skip {
				out = append(out, r.s[start:r.i]...)
			}
			simple = false
			r.i++
			if r.i >= len(r.s) {
				return "", r.fail("échappement tronqué")
			}
			e := r.s[r.i]
			if isDigit(e) {
				n := 0
				for k := 0; k < 3 && r.i < len(r.s) && isDigit(r.s[r.i]); k++ {
					n = n*10 + int(r.s[r.i]-'0')
					r.i++
				}
				if n > 255 {
					return "", r.fail("échappement décimal hors octet : %d", n)
				}
				if !skip {
					out = append(out, byte(n))
				}
				continue
			}
			if !skip {
				if m, ok := escapes[e]; ok {
					out = append(out, m)
				} else {
					out = append(out, e)
				}
			}
			r.i++
			continue
		}
		if !simple && !skip {
			out = append(out, c)
		}
		r.i++
	}
	return "", r.fail("chaîne non fermée")
}

func (r *reader) number() (Number, bool) {
	s := r.s
	j := r.i
	if j < len(s) && s[j] == '-' {
		j++
	}
	if j+1 < len(s) && s[j] == '0' && (s[j+1] == 'x' || s[j+1] == 'X') {
		k := j + 2
		for k < len(s) && (isDigit(s[k]) || (s[k] >= 'a' && s[k] <= 'f') || (s[k] >= 'A' && s[k] <= 'F')) {
			k++
		}
		if k > j+2 {
			v, err := strconv.ParseInt(string(s[r.i:k]), 0, 64)
			if err == nil {
				r.i = k
				return Number{I: v, IsInt: true}, true
			}
		}
		// Python : « 0x » sans chiffre retombe sur l'entier 0 puis bute sur « x ».
	}
	k := j
	for k < len(s) && isDigit(s[k]) {
		k++
	}
	intDigits := k - j
	isFloat := false
	if k < len(s) && s[k] == '.' {
		f := k + 1
		for f < len(s) && isDigit(s[f]) {
			f++
		}
		if intDigits > 0 || f > k+1 {
			isFloat = true
			k = f
		}
	}
	if intDigits == 0 && !isFloat {
		return Number{}, false
	}
	if k < len(s) && (s[k] == 'e' || s[k] == 'E') {
		e := k + 1
		if e < len(s) && (s[e] == '+' || s[e] == '-') {
			e++
		}
		if e < len(s) && isDigit(s[e]) {
			for e < len(s) && isDigit(s[e]) {
				e++
			}
			k = e
			isFloat = true
		}
	}
	txt := string(s[r.i:k])
	r.i = k
	if !isFloat {
		if v, err := strconv.ParseInt(txt, 10, 64); err == nil {
			return Number{I: v, IsInt: true}, true
		}
	}
	f, _ := strconv.ParseFloat(txt, 64)
	return Number{F: f}, true
}

func (r *reader) name() string {
	j := r.i
	if j >= len(r.s) || !isNameStart(r.s[j]) {
		return ""
	}
	for j < len(r.s) && isName(r.s[j]) {
		j++
	}
	return string(r.s[r.i:j])
}

func (r *reader) value(skip bool) (any, error) {
	r.blanks()
	if r.i >= len(r.s) {
		return nil, r.fail("valeur attendue")
	}
	c := r.s[r.i]
	switch {
	case c == '{':
		return r.table(skip)
	case c == '"' || c == '\'':
		return r.str(skip)
	}
	if c == '-' || c == '.' || isDigit(c) {
		if n, ok := r.number(); ok {
			return n, nil
		}
	}
	if w := r.name(); w != "" {
		r.i += len(w)
		switch w {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "nil":
			return nil, nil
		}
		r.i -= len(w)
		return nil, r.fail("identifiant inattendu « %s »", w)
	}
	return nil, r.fail("caractère inattendu « %c »", c)
}

func keyText(k any) (string, bool) {
	switch v := k.(type) {
	case string:
		return v, true
	case Number:
		if v.IsInt {
			return "", false
		}
		if v.F == math.Trunc(v.F) && !math.IsInf(v.F, 0) && math.Abs(v.F) < 1<<62 {
			return "", false
		}
		return strconv.FormatFloat(v.F, 'g', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	}
	return "", true
}

func (t *Table) put(k any, v any) error {
	switch key := k.(type) {
	case string:
		if _, ok := t.Str[key]; !ok {
			t.Order = append(t.Order, key)
		}
		t.Str[key] = v
		return nil
	case Number:
		if key.IsInt {
			t.Int[key.I] = v
			return nil
		}
		if key.F == math.Trunc(key.F) && !math.IsInf(key.F, 0) && math.Abs(key.F) < 1<<62 {
			t.Int[int64(key.F)] = v
			return nil
		}
	case nil:
		return errors.New("clé nil")
	}
	s, _ := keyText(k)
	if t.Other == nil {
		t.Other = map[string]any{}
	}
	t.Other[s] = v
	return nil
}

func (r *reader) table(skip bool) (any, error) {
	if r.depth >= MaxDepth {
		return nil, r.fail("imbrication au-delà de %d niveaux", MaxDepth)
	}
	r.depth++
	defer func() { r.depth-- }()
	if err := r.expect('{'); err != nil {
		return nil, err
	}
	var t *Table
	if !skip {
		t = newTable()
	}
	var positional []any
	for {
		r.blanks()
		if r.i >= len(r.s) {
			return nil, r.fail("table non fermée")
		}
		c := r.s[r.i]
		if c == '}' {
			r.i++
			break
		}
		if c == '[' {
			r.i++
			k, err := r.value(false)
			if err != nil {
				return nil, err
			}
			if err := r.expect(']'); err != nil {
				return nil, err
			}
			if err := r.expect('='); err != nil {
				return nil, err
			}
			v, err := r.value(skip)
			if err != nil {
				return nil, err
			}
			if !skip {
				if err := t.put(k, v); err != nil {
					return nil, r.fail("%v", err)
				}
			}
		} else {
			handled := false
			if w := r.name(); w != "" {
				j := r.i + len(w)
				for j < len(r.s) && (r.s[j] == ' ' || r.s[j] == '\t' || r.s[j] == '\r' || r.s[j] == '\n') {
					j++
				}
				if j < len(r.s) && r.s[j] == '=' && (j+1 >= len(r.s) || r.s[j+1] != '=') {
					r.i = j + 1
					v, err := r.value(skip)
					if err != nil {
						return nil, err
					}
					if !skip {
						_ = t.put(w, v)
					}
					handled = true
				}
			}
			if !handled {
				v, err := r.value(skip)
				if err != nil {
					return nil, err
				}
				if !skip {
					positional = append(positional, v)
				}
			}
		}
		r.blanks()
		if r.i < len(r.s) && (r.s[r.i] == ',' || r.s[r.i] == ';') {
			r.i++
		}
	}
	if skip {
		return nil, nil
	}
	for k, v := range positional {
		if _, ok := t.Int[int64(k+1)]; !ok { // setdefault : une clé explicite gagne
			t.Int[int64(k+1)] = v
		}
	}
	return t, nil
}

// Parse lit toutes les affectations du fichier. Seules les variables de `keep`
// sont construites en mémoire ; les autres sont vérifiées puis ignorées (c'est le
// cas de ForeverPulseScanDB, l'état interne de l'addon, jamais lu ni transmis).
// keep == nil : toutes les variables sont gardées.
func Parse(src []byte, keep map[string]bool) (map[string]any, error) {
	if bytes.HasPrefix(src, []byte("\xef\xbb\xbf")) {
		src = src[3:]
	}
	r := &reader{s: src}
	out := map[string]any{}
	for {
		r.blanks()
		if r.i >= len(r.s) {
			return out, nil
		}
		n := r.name()
		if n == "" {
			return nil, r.fail("nom de variable attendu")
		}
		r.i += len(n)
		if err := r.expect('='); err != nil {
			return nil, err
		}
		skip := keep != nil && !keep[n]
		v, err := r.value(skip)
		if err != nil {
			return nil, err
		}
		if !skip {
			out[n] = v
		}
		r.blanks()
		if r.i < len(r.s) && r.s[r.i] == ';' {
			r.i++
		}
	}
}

// Helpers de lecture typée ------------------------------------------------------

// AsTable renvoie la table ou nil.
func AsTable(v any) *Table { t, _ := v.(*Table); return t }

// AsString renvoie la chaîne et vrai si v est une chaîne.
func AsString(v any) (string, bool) { s, ok := v.(string); return s, ok }

// AsInt renvoie l'entier et vrai si v est un nombre entier (au sens Python : int).
func AsInt(v any) (int64, bool) {
	n, ok := v.(Number)
	if !ok || !n.IsInt {
		return 0, false
	}
	return n.I, true
}

// Str renvoie la chaîne ou "".
func Str(v any) string { s, _ := v.(string); return s }
