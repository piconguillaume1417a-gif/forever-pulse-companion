package store

import (
	"database/sql"
	"errors"
	"net/url"
	"strings"
)

// CumulScope keeps regional scopes distinct even if their public realm/mode/
// faction IDs are equal. Preserve the old label when its regional origin can
// be established. Never assign an ambiguous historical cumul to a new region.
func (w *FileWriter) CumulScope(scope, region string) (string, error) {
	region = strings.ToLower(strings.TrimSpace(region))
	var key string
	e := w.tx.QueryRowContext(w.ctx, "select cumul_key from cumul_scope_regions where scope_id=? and region=?", scope, region).Scan(&key)
	if e == nil {
		return key, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	var mapped, hasLegacy, known, matching int
	e = w.tx.QueryRowContext(w.ctx, "select count(*) from cumul_scope_regions where cumul_key=?", scope).Scan(&mapped)
	if e != nil {
		return "", e
	}
	e = w.tx.QueryRowContext(w.ctx, "select exists(select 1 from cumul_persos where scope_id=?)", scope).Scan(&hasLegacy)
	if e != nil {
		return "", e
	}
	if mapped == 0 && hasLegacy > 0 {
		e = w.tx.QueryRowContext(w.ctx, `select count(distinct lower(trim(coalesce(json_extract(x.value,'$.region'),'')))),
			coalesce(max(lower(trim(coalesce(json_extract(x.value,'$.region'),'')))=?),0)
			from files f,json_each(f.scopes) x where f.sha256<>? and x.key=?`, region, w.sha, scope).Scan(&known, &matching)
		if e != nil {
			return "", e
		}
	}
	key = scope
	if mapped > 0 || hasLegacy > 0 && (known != 1 || matching != 1) {
		key = scope + "@" + url.QueryEscape(region)
	}
	_, e = w.tx.ExecContext(w.ctx, "insert into cumul_scope_regions values(?,?,?)", scope, region, key)
	return key, e
}
