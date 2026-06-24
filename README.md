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

> **Scope:** Linux first (Steam/Proton, Lutris). macOS + Windows are a planned
> follow-up.

## Get it

Download the latest Linux/amd64 binary from [Releases](https://github.com/polarn/anjin-intel/releases/latest):

```sh
curl -fsSL -o anjin-intel \
  https://github.com/polarn/anjin-intel/releases/latest/download/anjin-intel-linux-amd64
chmod +x anjin-intel
```

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

**Install** (Linux) — registers a systemd *user* service that runs the shipper at
login and copies the binary to `~/.local/bin`:

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

**Run in the foreground** (no install — e.g. to try it, or on macOS/Windows):

```sh
anjin-intel run --server <url> --token <tok> --logdir <EVE/logs/Chatlogs> [--channels a,b]
```

`run` reads the install config when flags are omitted (that's how the service starts
it). It only ships lines written *after* it starts (no backfill); `--channels` is just
an optional offline/first-run seed. See [SPEC.md](SPEC.md) for the server contract.
