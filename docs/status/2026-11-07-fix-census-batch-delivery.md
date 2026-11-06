# Census delivery — 0.10.0-rc.8, 7 November 2026

## Change

Public rc.7 could send 501 small batches or 17 scopes in one request. The server
refused the request and the client marked every batch rejected, stopping automatic
delivery. rc.8 selects at most 500 whole batches and 16 scopes, includes only the
scopes referenced by those batches, and checks the complete serialized body.
Normal multi-batch requests stay within the existing 1 MiB target; a larger
singleton remains whole up to the existing 3 MiB hard transport bound.

An explicit Census `invalid_body` or `payload_too_large` response now bisects the
request until the affected whole batch is identified. Only that singleton is
marked rejected. Successful siblings and independent statistics continue. No
observation identifiers, dates, characters or logical contributions are fabricated.
The isolation and backlog pass is bounded to 32 HTTP attempts; unprocessed batches
stay pending for the normal retry scheduler.

Unknown or malformed Census errors remain retryable. Authentication, source and
protocol refusals retain durable global blocking. A valid acknowledgement still
must account for every transmitted batch before it can be marked sent. Rejected
census/statistics records now remain visible instead of reporting everything sent.
Other upload protocols keep their error classification; malformed JSON is never
treated as a valid refusal. FR and EN strings are included.

## Scope

Branch `fix/census-batch-delivery`, base public `main`/rc.7 at
`820e15f13a84f81220a3d17f8f0fd4298037e848`. Selected ordinary-delivery changes from
the prior local prototype were reviewed and strengthened with exact Census error
classification, malformed-response checks and bounded isolation. No experimental
fragment protocol, local-only executable mode or trial database code is included.

No server/database contract change, real queue reset, old rejection recovery,
credential change, `min_version` increase or forced update. Existing historical
rejections and blocks are preserved. A single scan above the server's per-scan
limits remains a separate problem; it is not split into artificial observations.

## Local validation

Go 1.24.13, Windows amd64, CGO disabled, synthetic loopback traffic and temporary
SQLite databases only:

- `go vet ./...`: PASS.
- `go test ./... -count=1`: PASS for every package. Optional real-file fixtures
  absent; no claim of real hosted ingestion. The subsequent final hint wording
  and UI test-model change were checked with `go test ./internal/i18n ./internal/vue -count=1`.
- Previously red 501-batch and 17-scope regression tests now PASS. Added 500/501/1,201
  packet boundary tests with a failure after the first committed packet and reopen
  of the same SQLite database; exact delivered batch set and chronological packet order.
- Lost response after simulated server commit, restart and duplicate acknowledgement:
  PASS without double acceptance or lost pending data.
- Mixed valid/invalid census plus statistics: PASS; only the invalid batch is
  rejected. Unknown 400/403 preserve pending state; auth/source/protocol blocks
  survive reopening; malformed errors cannot discard data.
- Existing 12 x 3,000-character byte-bound test PASS; whole-body checks, referenced
  scope pruning, separate factions, 413 bisection and bounded all-invalid isolation PASS.
- `go run ./outils/mkres 0.10.0-rc.8`; deterministic build with
  `go build -trimpath -buildvcs=false -ldflags "-H windowsgui -s -w"`: PASS.
  CLI and Windows resources report rc.8. Final executable SHA-256:
  `577cc6bde83f0c485cd082d9ebfdc9ab014908546e445381feaceeb91953a9da`.
- `outils/installateur/Build.ps1` and `Test.ps1` against the final local executable:
  PASS, native MSI search/costing/upgrade guards, extraction, payload hashes, per-user
  paths, startup ownership, uninstall boundaries and 24 layout cases. No installation
  transaction, uninstallation or normal Companion launch on the owner's PC.
  Live executable/startup remained unchanged.
- Manual diff review and `git diff --check`: PASS. No delegated agent review.

## Publication

The owner authorized correcting the Companion and testing locally before publication.
Local checks above precede pushing the candidate. Hosted CI, final merge, signed
update manifest and release verification will be recorded once actually completed.
Actual upgrade/repair/uninstall transactions run only on the isolated GitHub runner.
