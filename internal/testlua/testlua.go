// Package testlua écrit des SavedVariables pour les tests (miroir de simule.serialise).
package testlua

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// M : table Lua à clés chaînes. L : liste Lua. I : table Lua à clés entières
// explicites (« [1114] = … », comme le catalogue des arbres de talents).
type M = map[string]any
type L = []any
type I = map[int]any

func Serialise(v any) string {
	var b strings.Builder
	write(&b, v, 0)
	return b.String()
}

func str(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

func write(b *strings.Builder, v any, d int) {
	ind := strings.Repeat("\t", d+1)
	switch x := v.(type) {
	case nil:
		b.WriteString("nil")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case string:
		b.WriteString(str(x))
	case []string:
		l := make(L, len(x))
		for i, s := range x {
			l[i] = s
		}
		write(b, l, d)
	case L:
		b.WriteString("{\n")
		for _, e := range x {
			b.WriteString(ind)
			write(b, e, d+1)
			b.WriteString(",\n")
		}
		b.WriteString(strings.Repeat("\t", d) + "}")
	case M:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("{\n")
		for _, k := range keys {
			b.WriteString(ind + "[" + str(k) + "] = ")
			write(b, x[k], d+1)
			b.WriteString(",\n")
		}
		b.WriteString(strings.Repeat("\t", d) + "}")
	case I:
		keys := make([]int, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		b.WriteString("{\n")
		for _, k := range keys {
			b.WriteString(ind + "[" + strconv.Itoa(k) + "] = ")
			write(b, x[k], d+1)
			b.WriteString(",\n")
		}
		b.WriteString(strings.Repeat("\t", d) + "}")
	default:
		panic(fmt.Sprintf("type non géré %T", v))
	}
}

// Fichier : « ForeverPulseCensusDB = … » plus un ForeverPulseScanDB factice.
func Fichier(db M) []byte {
	return []byte("ForeverPulseCensusDB = " + Serialise(db) + "\nForeverPulseScanDB = {\n\t[\"etat\"] = { 1, 2, { [\"x\"] = \"secret interne\" } },\n}\n")
}
