# Guild roster upload candidate — 7 November 2026

Version 0.10.0-rc.10; not published or installed at preparation.
Reads `ForeverPulseGuildDB` from addon 4.6.2-test.3 as data, preserving full names,
GUIDs, levels, class and true/false/unknown presence. No ScanDB, notes or officer
data are transmitted. A complete snapshot is rejected atomically if inconsistent.

SQLite keeps the latest snapshot per region/scope/guild. Older backups never
replace it; unchanged snapshots never requeue; acknowledgments target the exact
version, and pending data survives restart. Upload runs sequentially after the
existing streams under the same mutex. The website's dedicated endpoint provides
an exact JSON receipt. Network errors back off, quota honors Retry-After, and a
source/schema refusal pauses only guild uploads. Diagnostics include guild counts.
The new source never modifies the census cumulative tally.

Local `go test ./...` and `go vet ./...` passed before the final context-preservation
and queue regression additions; targeted guild/app/store regressions then passed.
Final full test/build/installer verification and CI are still required before release.
Synthetic tests cover offline/unknown, incomplete rows, duplicate identities,
future time, reordered rows, restart, stale backup/ack, newer replacement, wipe,
HTTP outage/retry, sequential ordering and unchanged file reread. No real data
or credentials are included. The site must be deployed before real uploads.
