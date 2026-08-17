# anjin-intel

A tiny, **stdlib-only** agent that tails your EVE Online chat logs and ships intel
lines to your [anjin](https://github.com/polarn/anjin) server, which alerts you when
a hostile is reported near where your character is.

**Why it exists:** ESI (EVE's API) exposes no in-game chat. Chat lives only as files
the EVE *client* writes to local disk while you're logged in. A server can never pull
it — so this agent runs on your PC, tails the logs, and POSTs the lines.

**Why it's open + dependency-free:** it reads your chat and sends it to a server, so
you should be able to verify exactly what it does. It's MIT-licensed, pure Go standard
library (trivially auditable, reproducible), and **read-only** — it tails the log
directory and POSTs; it never writes to the game and never touches anything but the
channels you explicitly allow. Default is *no* channels.

> **Scope:** Linux (Steam/Proton, Lutris) and Windows. On Windows there's no
> background install yet — you run it in a terminal; see below. macOS is a planned
> follow-up.

## Get it

Grab the binary for your OS from [Releases](https://github.com/polarn/anjin-intel/releases/latest).

**Linux:**

```sh
curl -fsSL -o anjin-intel \
  https://github.com/polarn/anjin-intel/releases/latest/download/anjin-intel-linux-amd64
chmod +x anjin-intel
```

**Windows** (PowerShell):

```powershell
Invoke-WebRequest -OutFile anjin-intel.exe `
  https://github.com/polarn/anjin-intel/releases/latest/download/anjin-intel-windows-amd64.exe
```

The binary isn't code-signed (a certificate is hard to justify for a tool with a
handful of users), so Windows SmartScreen may warn on first run: **More info → Run
anyway**. The `SHA256SUMS` + provenance attestation below are the real check.

Each release ships a `SHA256SUMS` and a [SLSA build provenance](https://slsa.dev)
attestation, so you can verify the binary came from this repo's CI (not a hand-built
upload):

```sh
gh attestation verify anjin-intel --repo polarn/anjin-intel
```

Or build from source (pure Go stdlib, no deps):

```sh
go build -o anjin-intel ./cmd/anjin-intel
```

## Usage

### Linux

**Install** — registers a systemd *user* service that runs the shipper at login and
copies the binary to `~/.local/bin`:

```sh
anjin-intel install \
  --server https://anjin.example.net \
  --token  <enrollment-token-from-the-Intel-tab>
  # --logdir is auto-detected (Steam/Proton, Lutris, native); pass it if detection fails
```

Then manage everything from the **Intel tab**: tick the channels to monitor (the
shipper reports the ones it sees, polls the allowlist ~60s, and ships only those).

```sh
anjin-intel status      # installed? running? server reachable? last ship?
anjin-intel uninstall   # stop + remove the service, binary and config
```

### Windows

There's no `install` on Windows yet, so run it in a terminal and leave it open:

```powershell
.\anjin-intel.exe run --server https://anjin.example.net --token <enrollment-token>
```

`--logdir` is optional: the Chatlogs directory is found by asking Windows where your
Documents folder actually is. That matters because the folder is both **localized**
(it's `Dokument` on a Swedish install) and **relocatable** — OneDrive moves it under
`%USERPROFILE%\OneDrive` by default on Windows 11 — so there's no single path to
hardcode. If you have both a live and an old copy, the one EVE most recently wrote to
wins. Pass `--logdir` to override.

Then pick your channels in the **Intel tab**, same as Linux.

### Run in the foreground (any OS)

```sh
anjin-intel run --server <url> --token <tok> [--logdir <EVE/logs/Chatlogs>]
```

`run` reads the install config when flags are omitted (that's how the service starts
it), and auto-detects `--logdir` when neither supplies one. It only ships lines written
*after* it starts (no backfill); `--channels` is just an optional offline/first-run
seed. See [SPEC.md](SPEC.md) for the server contract.
