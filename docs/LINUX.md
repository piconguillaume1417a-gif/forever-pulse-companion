# Linux build

This fork adds a native Linux window to Forever Pulse Companion. The layout
engine (`internal/vue`) is unchanged; `internal/webui` renders its drawing
operations and hit zones in a WebKitGTK window with an HTML canvas.

## Dependencies

- Go 1.24 or newer
- a C compiler and `pkg-config`
- GTK 3 and WebKitGTK 4.1 development headers
- `secret-tool` from libsecret for token storage (optional; a `0600` file is
  used when it is absent)

Debian/Ubuntu:

```sh
sudo apt install gcc pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev libsecret-tools
```

Fedora:

```sh
sudo dnf install gcc pkgconf-pkg-config gtk3-devel webkit2gtk4.1-devel libsecret
```

## Build and run

```sh
make build
./dist/forever-pulse-companion
```

Or directly:

```sh
go build -trimpath -buildvcs=false -ldflags "-s -w" \
  -o dist/forever-pulse-companion ./cmd/forever-pulse-companion
```

Without cgo (`CGO_ENABLED=0`) the old headless foreground loop is built instead.
It has no window and no GUI actions.

## What works

- The full companion window, including hover, tooltips, keyboard focus
  (`Tab`, arrows, `Enter`, `Space`), language switching and dialogs.
- A StatusNotifierItem tray icon with the same menu as the Windows build.
  Closing the window keeps the companion running; the icon reopens it.
- Native GTK folder chooser for the World of Warcraft directory.
- WoW discovery under Wine, Steam/Proton and `~/Games`.
- Single instance. Launching the program again raises the hidden window.
- `--quit` closes the running instance; `--menu` raises it.
- Start at login through `~/.config/autostart/forever-pulse-companion.desktop`.
  The switch state is saved in `config.toml`, not just in the desktop file.
- Token storage in the Secret Service via libsecret's `secret-tool` (GNOME
  Keyring, KWallet). If `secret-tool` is absent, a `0600` fallback file under
  `$XDG_CONFIG_HOME/ForeverPulse/Companion/cles/` is used; the directory is kept
  `0700` and the mode is tightened on every write. The token is never written to
  `config.toml`. `WOWSYNC_TOKEN` still overrides both.
- Dark theme follows `GTK_THEME` / the GNOME `color-scheme` setting.
- Configuration persists in `config.toml`; the upload token persists in the
  system keyring and survives restarts.
- Desktop notifications with `notify-send`.

## What is not implemented

- **Tray host.** The icon needs a StatusNotifier host: KDE, XFCE, Cinnamon,
  Quickshell or Waybar show it directly; GNOME Shell needs an AppIndicator
  extension. Without a host the companion still runs, but closing the window
  quits it. On bars that hide new icons in a drawer, pin the Companion in the
  bar settings once.
- **Automatic updates.** Windows only in this version. The Linux window does
  not start the update loop, and `auto_update` defaults to `false` in its
  `config.toml`. The explicit `--update-check` diagnostic still reads the
  manifest but never installs anything. AppImage or another channel can come
  later.
- **Installer.** Build from source or package the binary with your distribution
  or an AppImage.

## Shared changes with Windows

- `internal/config` keeps the Windows defaults in `platform_windows.go` and the
  Linux defaults (game folder, autostart comment, no auto-update) in
  `platform_other.go`. The Windows binary keeps its previous defaults.
- `watch.Racine` now strips trailing `/` and `\` before `filepath.Clean` and
  reapplies the separator when the path is a bare volume such as `C:`. This
  accepts a folder pasted with a trailing backslash on Linux without changing
  the root detection on Windows.
- `cmd/forever-pulse-companion/main.go` passes the data directory to
  `quitRunning` and `menuRunning`. The Windows implementations ignore the new
  argument; Linux uses it to find the single-instance socket.

## Tests

```sh
go test ./...
```

## CI job

The fork token used to push this branch lacked the GitHub `workflow` scope, so
the Linux job is kept here instead of `.github/workflows/build.yml`. Add it
under `jobs:` when the workflow scope is available:

```yaml
  linux:
    runs-on: ubuntu-24.04
    env:
      CGO_ENABLED: '1'
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24.13'
          cache: false
      - name: Install WebKitGTK
        run: |
          sudo apt-get update
          sudo apt-get install -y gcc pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
      - name: Vet
        run: go vet ./...
      - name: Test
        run: go test ./... -count=1
      - name: Build companion
        run: |
          mkdir -p dist
          go build -trimpath -buildvcs=false -ldflags "-s -w" -o dist/forever-pulse-companion ./cmd/forever-pulse-companion
```
