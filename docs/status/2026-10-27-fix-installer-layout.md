# Windows installer layout — 27 October 2026

The installation folder used a single-line field inside a fixed-position dialog.
Long paths could be hidden, and larger text or a smaller window had no layout
fallback. The wizard now uses content-sized rows, a selectable read-only path that
wraps and grows with its native line count, and a resizable window. The middle
section scrolls when needed; installation and close buttons stay outside it.

The MSI operations, installation directory and Companion executable are unchanged.
The local candidate packages the existing 0.10.0-rc.6 executable, SHA-256
`B71FFA9B70DF217E959ED3BB7CB8E493172A131384855BB9D3DE4B485DD749AE`.

## Local validation

- `Build.ps1 -Exe <existing rc.6 executable> -OutDir .go-tmp/after`: PASS,
  C# compiler warnings treated as errors.
- `Test.ps1` with the same arguments: PASS. Native MSI search, launch conditions,
  costing, package extraction and hashes, upgrade guards, shortcut ownership and
  uninstall boundaries checked without an installation transaction. Live binary
  and startup entry unchanged.
- 24 native WinForms cases PASS: FR/EN, widths 550/650/900 logical pixels,
  fonts 10/14 points, current path and a synthetic 260-character path. Checks cover
  text height, final path line/column, body row separation, button visibility and
  scrolling to the completion status. Normal preview and long-path FR/EN captures
  inspected visually. All captures remain under ignored `.go-tmp/`.
- The layout harness rejects the previous installer because its path field cannot
  wrap. Tests run at the host's DPI; enlarged fonts do not prove every Windows DPI
  setting or moving the window between monitors of different scales.
- `git diff --check`: PASS.

Candidate setup SHA-256:
`CB1E74CFDB5BD28B0F0D3628AD77BB3B391D7DED8C155E3F19719FC6D82B63C1`.

This is a local installer candidate. No release asset or update manifest replaced,
no real installation/uninstallation performed, and no Companion process started
or stopped. Hosted CI and release publication are separate from these local checks.
