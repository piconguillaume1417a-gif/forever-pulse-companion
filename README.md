# Forever Pulse Companion

Windows companion of the **Forever Pulse** addon for *World of Warcraft: Forever*. It reads
the SavedVariables written by the addon and uploads them to https://forever-pulse.com.
It never touches the game. Full guide in French: [LISEZMOI.md](LISEZMOI.md).

- **Download**: [ForeverPulseCompanion-Setup.exe](https://github.com/piconguillaume1417a-gif/forever-pulse-companion/releases/latest/download/ForeverPulseCompanion-Setup.exe)
  from the [latest release](https://github.com/piconguillaume1417a-gif/forever-pulse-companion/releases/latest)
  (Windows 11 x64, per-user, no administrator rights; English or French). Uninstall from
  Windows Settings → Apps; settings and readings are kept.
- **Automatic updates** (0.10.0): a daily signed check of the latest release, installed on next
  start or from the tray menu, with automatic rollback. See [docs/MISE-A-JOUR-AUTO.md](docs/MISE-A-JOUR-AUTO.md).
- **Code signing**: free code signing provided by [SignPath.io](https://about.signpath.io/),
  certificate by [SignPath Foundation](https://signpath.org/) (application pending: releases are
  unsigned until approval; Windows Smart App Control may block them). Policy: [CODE-SIGNING.md](CODE-SIGNING.md).
- **Privacy**: [privacy policy](CODE-SIGNING.md#privacy-policy). Remove an observed character from the
  site: [foreverpulsesupport@gmail.com](mailto:foreverpulsesupport@gmail.com).
- **License**: [MIT](LICENSE).

Build: `go build -trimpath -ldflags "-H windowsgui -s -w" -o ForeverPulseCompanion.exe ./cmd/forever-pulse-companion` (Go 1.24).

## Browser connection candidate — 0.9.0-rc.1

Click **Connect to Forever Pulse**, sign in or create an account in your browser,
compare the code and explicitly approve this installation. The Companion saves
its limited upload credential in Windows Credential Manager and resumes uploads.
No token copy is required. **Manage installations** shows last use and individual
revocation; signing out of the browser does not revoke a PC. Manual paste remains
an advanced fallback. English is the default; FR is selectable and remembered.

This is a prepared candidate, not an installed or publicly activated release.
The account/SMTP/site release gates still apply. Existing credentials, settings,
five-minute/custom polling and persistent queues are preserved. The dated 0.8.0
sections below describe earlier delivery history, not current installed state.
See [candidate validation, update and rollback](docs/COMPANION-CONNECT-0.9.0.md).

## Crossed players' specialisations — branch `feat/talent-specs` (6 October 2026)

With addon **4.2.0**, each statistics sheet may carry the character's latest
talent capture (`ta`) and the file a talent tree catalogue (`arbres`,
`talents_v = 1`). The companion reads them, checks the website's bounds locally
and uploads statistics as **HTTP schema 2** (`X-Schema: 2`, `"schema": 2`):
schema 1 + `talent_trees` + `file.talents_version` + `characters[].talents`.
An unreadable or out-of-bounds capture is dropped and counted while its sheet is
still sent; a sheet without talents keeps exactly its 0.9.0-rc.1 payload and digest.

**Fallback:** while the website only knows schema 1 (409 whose `supported` lists
1 but not 2), the same request is immediately resent as schema 1 without talents
or trees; schema 2 is retried no earlier than 6 hours later (mode stored in
`companion.db`, kept across restarts, shown in the status details). Once schema 2
is accepted again, sheets sent as schema 1 with talents are queued again once. A
409 that does not list schema 1 stops statistics as before. See
[talent validation](docs/VALIDATION-TALENTS.md) (French).

## Windows 11 installer (0.8.0)

Run **ForeverPulseCompanion-0.8.0-Setup.exe** for the French installer. It contains the complete offline Windows Installer package for **Windows 11 x64**, installs for the current user in `%LOCALAPPDATA%\Programs\ForeverPulseCompanion`, adds a Start menu shortcut and an optional desktop shortcut, and preserves configuration, queues, logs and the existing Windows credential. No administrator elevation is requested. The package is **unsigned**; Windows application control may reject it.

Quit the running companion from its tray menu before installing. The installer never stops a process or starts the companion automatically. Click **Ouvrir** after installation to start 0.8.0, or **Fermer** to start it later from the Start menu. Launching the app can resume uploads using the existing token. On its first launch, the new copy updates the Windows startup entry according to the existing app configuration. Building/testing the installer leaves the repository's in-service 0.7.4 executable untouched.

To uninstall **while keeping data**, quit the companion and use **Windows Settings → Apps → Installed apps → Forever Pulse Companion → Uninstall**. Windows removes only installed program files, shortcuts and its own startup entry. The companion's existing in-app **Uninstall…** action still deletes application data and the credential; it is a different operation. Build and verification instructions are in [outils/installateur/README.md](outils/installateur/README.md).

## Auction prices (0.8.0, 3 October 2026)

The candidate **ForeverPulseCompanion-0.8.0.exe** implements `CONTRAT-HDV-v1.md` revision 2. It is **not installed**: the running `ForeverPulseCompanion.exe` remains 0.7.4 and `master` remains at `2e642d4`. This delivery changes neither the addon nor the website.

The companion discovers current `Auctionator.lua` files alongside Forever Pulse SavedVariables, with the same `poll_seconds`, three-second stability and shared, unlocked reads. The authoritative contract **L1 excludes Auctionator.lua.bak**. Lua is parsed as data; only `AUCTIONATOR_PRICE_DATABASE` is built in memory. The independent CBOR decoder follows [RFC 8949](https://www.rfc-editor.org/rfc/rfc8949.html), accepts database 8 / realm 2, and rejects unsupported encodings, tags, floats, indefinite lengths, trailing bytes, duplicate field keys, depth above 8, more than 200,000 elements and strings above 8 MiB.

The sibling `ForeverPulse.lua` supplies `hdv` v2 notes and the optional object catalogue. Each source day must have one proven market throughout the conservative three-day window, including interface-load time. Missing or ambiguous notes, unknown hotels and incomplete identities produce counts and no price rows. A missing low price stays unknown. The undated current price accompanies only rows on the realm key's greatest source day. Days remain raw source keys with `auctionator/<version>/db8/unverified`, never invented UTC observation dates.

Four new SQLite tables persist file signatures, candidates and their last exact acknowledgements, immutable upload parts, and per-market delays. Only new or modified rows and catalogue items leave; older files cannot overwrite newer candidates, and loss of the current price alone does not trigger an upload. New ambiguous notes or truncated reads withdraw affected unacknowledged prices. The 0.7.4 tally still loads only while a census file is processed.

One update contains one market, uses SHA-256 of `file.sha256|market.id` as `update_id`, and is split into requests below 3,000,000 decompressed bytes, 5,000 price rows and 5,000 catalogue items. Only the exact JSON acknowledgement confirms rows. Invalid rows stay rejected until changed; expired rows leave the queue; capacity refusals wait at least six hours. A missing route (404), disabled source (403), incompatible source or market conflict (409), and network failures do not block census or statistics. Unsupported schema waits for a new companion version; market conflict stops automatic retries.

`429/Retry-After` survives restart and is never shortened or bypassed by **Send now**. Each market proceeds independently; new updates wait at least 30 minutes and parts of the same update can follow within ten minutes. The server remains authoritative. The window shows sent/pending price rows and days without proven faction; its tooltip and `--status` expose counts, the last acknowledgement and last error. `--once Auctionator.lua` and `--dry-run --out <directory> [files]` send nothing and do not read the Windows token. Dry-run uses a disposable database; remove any exported diagnostic bodies afterwards. Put options before file paths.

Real offline check on 3 October: two current files, database 8, five realm keys with version 2; **5,721 objects including 3,117 on PvP** (earlier estimate: about 2,994), seven days without a note, zero attributed days, zero sendable rows, **zero requests built or posted**, zero unreadable days. No real data copied or retained; output directory removed. Hosted ingestion and source activation remain unverified.

`ForeverPulseCompanion.exe` sends the readings written by the **Forever Pulse** addon in WoW: Forever to the Forever Pulse website, **https://forever-pulse.com**. It has a small window and runs in the background with an icon next to the clock. It never touches the game. The full guide, in French, is [LISEZMOI.md](LISEZMOI.md).

## Ten times less memory at rest (0.7.4)

The tally of distinct observed characters (about 200,000 characters, up to 220 MB in memory) is no longer kept between two `/reload`s. It is read back from `companion.db` only while a new file is integrated, then released and handed back to Windows. The “N distinct characters observed since …” lines in the window and `--status` are counted directly in the database (same totals and dates, checked on the real database). MEASURED on 3 October: 358 MB at rest in 0.7.3, **39 MB** in 0.7.4; startup peak 727 MB then 253 MB; `--status` 2.9 s then 1.7 s. Processing a new file still takes a few hundred MB for a few seconds, released right away.

## File check every 5 minutes (0.7.3)

At the owner's request `poll_seconds` defaults to 300; the old defaults 600 (0.6.0 to 0.7.1) and 30 (0.7.2) become 300 once, any other value chosen by hand is kept. A new reading leaves at most about 5 minutes after `/reload` or logout; **Send now** stays immediate.

## Faster automatic upload (0.7.2)

The file check now runs every 30 seconds instead of 10 minutes (`poll_seconds = 30`; the old default 600 is replaced once, any other value chosen by hand is kept). A check only looks at the size and date of the files and opens none of them; the full read (5 to 11 MB) happens only after the game really wrote the file. A new reading therefore leaves about 35 seconds after `/reload` or logout. `--store-token` now clears the “token” block like the **Paste token** button, so the icon no longer stays on “Token missing or refused” and uploads resume on their own.

## Smaller uploads (0.7.1)

First real upload to production on 3 October: 1,758 batches and 4,415 statistics sheets accepted, none refused. The largest census requests (about 19,000 characters, 2.7 MB) took up to 27 s on the site, whose limit is 30 s, so each census request is now capped at 1 MB (about 7,000 characters). Statistics keep the 3 MB cap (at most 4 s measured).

## Statistics (0.7.0)

Since 0.7.0 the companion also sends the character statistics read by the addon (`ForeverPulseStatsDB`) to `<site_url>/api/ingest/stats`, with the same token as the census. Each sheet (one character in one game context) goes through a persistent queue in `companion.db`: it is marked sent only on the route's exact JSON acknowledgement (`accepted + duplicates + rejected` equal to the number of sheets), it survives a restart, and a sheet older than or identical to the last one sent is not queued again. A sheet refused alone does not block the others; a refusal of the statistics source (`403`, `409`) does not block the census. A zero stays a zero, an absent statistic is never sent as zero, an unreadable value is dropped and counted. Numbers travel as decimal text, digit for digit. The window and `--status` show, for each flow, the file followed, the last read, the last confirmed upload, what is pending and the last error.

On 2 October the 0.7.0 candidate decoded a real addon 3.9.5 file (24.6 MB) offline with `--once`: 1,193 sendable batches, 710,359 character rows, 4,089 sendable statistics sheets (208,295 values), 7 sheets without a value, 0 unreadable value. `--dry-run` built 35 census requests and 2 statistics requests and posted nothing.

## Upload route check (0.6.4)

The POST always goes to `<site_url>/api/ingest/census`: in production `site_url` is `https://forever-pulse.com` and the request goes to `https://forever-pulse.com/api/ingest/census`. A `site_url` with a path (including `/api/ingest/census`, which the companion adds itself), a query, credentials, plain `http://` outside the local machine or a Supabase host is refused before any connection; `--dry-run` prints the exact destination. Redirects are never followed, so the body and token never leave for another host. A batch is marked sent only on the route's exact JSON acknowledgement: `202` when at least one batch is new, `200` when all are duplicates, and `accepted + duplicates + rejected` equal to the number of batches sent. A Vercel protection page, an HTML page, a redirect or a bare `200` leaves the batches pending; blocking refusals (`400`, `401`, `403`, `409`, `413`) are only taken from the route's JSON error. On 29 September, 0.6.4 replaced the installed `ForeverPulseCompanion.exe` while the companion was closed (it was not started); a copy of 0.6.3 is kept as `ForeverPulseCompanion-0.6.3-backup.exe`. `python outils/etat-file-locale.py` prints the local queue as counts only, from a consistent copy of the WAL database, without network.

## Upload identifier (0.6.3)

Addon 3.7.9 keeps `observer.id` in its local SavedVariables across sessions. Version 0.6.3 puts a fresh random UUID v4 in the historical HTTP field `file.observer_session_id` for each request. The website also replaces that field before storage. The stable observer identity at the website is the companion token. The installed executable was replaced with 0.6.3 after the old process exited normally; a copy of 0.6.2 is kept as `ForeverPulseCompanion-0.6.2-backup.exe`.

On 28 September, the separate 0.6.3 candidate executable ran successfully on both large beta SavedVariables files with `--once` (schema 6, no upload). The installed 0.6.3 executable has the same SHA-256 as that candidate and its own `--once` run on the larger beta file exited successfully. This local decode check does not verify a real HTTP upload.

## Website (0.6.2)

Since 0.6.2 (28/09/2026) the website is `https://forever-pulse.com` (with a hyphen), the default `site_url` in `%APPDATA%\ForeverPulse\Companion\config.toml`. On first start of 0.6.2, a config that still had the old default (`http://localhost:3000`) is moved to it once; the old name `foreverpulse.com` (no hyphen) is always replaced; any other address you set by hand is kept. A batch is only marked as sent when the reply really comes from the site's upload route (a JSON acknowledgement with `accepted`): a landing page or a domain host answering 200 loses nothing, the batch is retried later.

## Language

The window, the icon menu and the notifications are available in **French and English**. The companion starts in English until another language is chosen. Change it at any time with the **FR / EN** switch in the window; the choice applies immediately and is saved in `config.toml` (`language = 'fr'` or `'en'`). The technical log stays in French.

## The window (0.6.0)

The window follows the Windows light or dark theme and opens next to the clock. It shows a status card (green, orange or red, with what to do next), three counters (batches sent, pending, rejected), the last upload, the distinct characters observed per scope, and the actions: **Send now**, **Connect to Forever Pulse** (highlighted when disconnected), **Manage installations** (your account on the website), **Open log**, a **Start with Windows** switch, **FR / EN**, and at the bottom the rare actions (**Clear data…**, **Uninstall…**) plus **Minimize** and **Quit**. Every button has a tooltip. Keyboard: Tab / Shift+Tab or arrows to move, Enter or Space to activate, Esc to close.

The window only exists while it is open: closing it frees everything it uses, and when started with Windows the companion does not create it until the icon is clicked. The watch loop wakes up every 5 minutes (`poll_seconds` in `config.toml`, 1 to 3600) and only checks the file size and date and whether batches are waiting; a file that just changed is re-checked 3 seconds later, then processed, and **Send now** wakes the loop immediately; memory used to decode a large file and read the tally back is handed back to Windows as soon as it is processed; at rest the companion uses about 40 MB (0.7.4).

## Everyday use

- **Left click** on the icon next to the clock: opens the window.
- **Right click**: menu with the current status, **Open Forever Pulse Companion** (bold, same as left click), **Send now**, **Connect to Forever Pulse**, **Cancel connection**, **Manage installations**, **Open log**, **Start with Windows** and **Quit**.
- **Minimize**, the title bar “—” button or the close button: the window goes straight to the notification area, with no notification and no taskbar button. The companion keeps running.
- **Start with Windows**: checked by default, can be unchecked (per-user `HKCU\...\Run` entry, no administrator rights).
- **Connect to Forever Pulse**: shows an 8-character code (large in the window, in a notification and in the tray menu) and opens the website page; check that the page shows the same code, then approve. No token copy. The **Paste token** button was removed in 0.10.0: the website no longer issues manual tokens.
- **Manage installations**: opens your account (`https://forever-pulse.com/account`, Companion panel) to see and revoke installations.
- **Clear local data…**: empties the upload queue, the tally of observed characters and the list of files already read. The token and settings are kept.
- **Uninstall…**: removes the start with Windows entry, the stored token, `%APPDATA%\ForeverPulse\Companion\` and the program itself. Then revoke this installation from your account on the website.

## What is sent

`ForeverPulseCensusDB` sends observed characters to `/api/ingest/census` (schema 4); `ForeverPulseStatsDB` sends character statistics and their name catalogue to `/api/ingest/stats` (schema 1; schema 2 on branch `feat/talent-specs` adds each character's latest talent capture and the talent tree catalogue, never the inspected player's loadout name). The format remains `docs/data-contract.md` in the website repository. Since 0.8.0, authorised prices from `AUCTIONATOR_PRICE_DATABASE` and objects from `ForeverPulseStatsDB.objets` v1 go to `/api/ingest/auctions` (schema 1), according to `CONTRAT-HDV-v1.md` revision 2: file hash and versions, complete proven market identity, item number, raw source day and provenance, known low/high/current prices as decimal text, known quantity including zero, and optional object metadata. No price observation timestamp is fabricated.

## What is never sent

`ForeverPulseScanDB`, chat, Battle.net/account identifiers, paths and companion settings never enter upload bodies. Neither do Auctionator's personal variables or any of their values: `AUCTIONATOR_POSTING_HISTORY`, `AUCTIONATOR_SHOPPING_LISTS`, `AUCTIONATOR_RECENT_SEARCHES`, `AUCTIONATOR_SELLING_GROUPS`, `AUCTIONATOR_CONFIG`, `AUCTIONATOR_SAVEDVARS`, `AUCTIONATOR_CHARACTER_CONFIG`, `AUCTIONATOR_VENDOR_PRICE_CACHE`. The price upload contains no raw `hdv` note timestamps, client clock offset, timezone, interface-load time or map ID, no unproven day, no invented low price, and no `occurred_at`. Upload credentials travel in the authentication header. Browser-assisted connection returns a credential only to the native client over its private polling response, then stores it in the Windows vault. The companion reads `ForeverPulse.lua`, its backup and current `Auctionator.lua`; it never writes, renames or deletes anything in the game folder.

Since 0.4.0 the companion also reads the compact format written by addon 3.7.0 (schema 5: shared dictionaries, coded `/who` batches, channel deltas). It expands it before any check, so what reaches the website is exactly what the same capture would have produced in the previous format (schema 4).

Since 0.5.0 it also reads schema 6 (addon 3.7.1), where names are stored once in a shared table and rows only carry their number. It is expanded to schema 4 before upload as well.
