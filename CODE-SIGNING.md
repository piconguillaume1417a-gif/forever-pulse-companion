# Code signing policy

Windows releases of Forever Pulse Companion are meant to be signed.

Free code signing provided by [SignPath.io](https://about.signpath.io/), certificate by
[SignPath Foundation](https://signpath.org/) — **pending approval**: releases published
before approval are unsigned.

## Team roles

- Committers and reviewers: [piconguillaume1417a-gif](https://github.com/piconguillaume1417a-gif)
- Approvers: [piconguillaume1417a-gif](https://github.com/piconguillaume1417a-gif)

Only binaries built by this repository's GitHub Actions workflow from its own source
code are submitted for signing. Every signing request is approved manually.

## Update signature

Independently of Authenticode, each release ships `latest.json` and `latest.json.sig`:
an Ed25519 signature checked by the Companion with the public key embedded in
`internal/update/cle.go` before any automatic update is downloaded or installed.

## Privacy policy

This program will not transfer any information to other networked systems unless
specifically requested by the user or the person installing or operating it, except:

- the readings written by the Forever Pulse addon, sent to https://forever-pulse.com
  with the installation's upload credential: characters seen in public channel rosters
  and `/who` responses (name, class, race, sex, level, realm, zone, guild, faction),
  character statistics and talents, and auction prices when that source is enabled;
- a daily check of the latest release on github.com (no credential, no game data);
  it can be disabled with `auto_update = false` in `config.toml`.

Never collected: chat, private or custom channels, Battle.net or account identifiers,
file paths, Companion settings, passwords.

- **Stop sending**: revoke the installation from your Forever Pulse account.
- **Erase local data**: "Uninstall…" in the Companion deletes its credential and local data.
- **Remove an observed character from the site**: email
  [foreverpulsesupport@gmail.com](mailto:foreverpulsesupport@gmail.com); no account needed.
