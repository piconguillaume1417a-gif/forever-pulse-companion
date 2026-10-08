package auctions

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"wowsync/internal/lua"
)

type Item struct {
	ID       int64   `json:"item_id"`
	Name     string  `json:"name"`
	Quality  *int64  `json:"quality"`
	Class    *int64  `json:"class_id"`
	Subclass *int64  `json:"subclass_id"`
	Level    *int64  `json:"item_level"`
	Required *int64  `json:"required_level"`
	Vendor   *string `json:"vendor_price"`
}
type Catalog struct {
	Version  int     `json:"version"`
	Locale   *string `json:"locale"`
	Build    *int64  `json:"build"`
	Exported *string `json:"exported_at"`
	Items    []Item  `json:"items"`
}

func ReadCatalog(t *lua.Table) *Catalog {
	if t == nil {
		return nil
	}
	v, ok := lua.AsInt(t.Get("v"))
	if !ok || v != 1 {
		return nil
	}
	raw, ok := t.Get("items").(string)
	if !ok || len(raw) > 2<<20 {
		return nil
	}
	c := &Catalog{Version: 1}
	locale := lua.Str(t.Get("locale"))
	if regexp.MustCompile(`^[a-z]{2}[A-Z]{2}$`).MatchString(locale) {
		c.Locale = &locale
	}
	if n, ok := lua.AsInt(t.Get("build")); ok && n > 0 {
		c.Build = &n
	}
	if n, ok := lua.AsInt(t.Get("t")); ok && n > 0 && n <= 253402300799 {
		s := time.Unix(n, 0).UTC().Format(time.RFC3339)
		c.Exported = &s
	}
	seen := map[int64]bool{}
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		p := strings.SplitN(line, "|", 8)
		if len(p) != 8 {
			return nil
		}
		id, ok := integer(p[0], 1, 999999999)
		if !ok || seen[id] || !textCatalog(p[7]) {
			return nil
		}
		seen[id] = true
		x := Item{ID: id, Name: p[7]}
		fields := []**int64{&x.Quality, &x.Class, &x.Subclass, &x.Level, &x.Required}
		hi := []int64{8, 50, 100, 1000, 100}
		for i, f := range fields {
			if p[i+1] != "" {
				n, ok := integer(p[i+1], 0, hi[i])
				if ok {
					*f = &n
				}
			}
		}
		if n, ok := integer(p[6], 0, 999999999999999); ok {
			s := strconv.FormatInt(n, 10)
			x.Vendor = &s
		}
		c.Items = append(c.Items, x)
		if len(c.Items) > 20000 {
			return nil
		}
	}
	if len(c.Items) == 0 {
		return nil
	}
	return c
}
func textCatalog(s string) bool {
	return len(s) > 0 && len(s) <= 160 && utf8.ValidString(s) && strings.IndexFunc(s, func(r rune) bool { return r < 32 || r == 127 }) < 0
}
