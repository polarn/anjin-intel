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

## Usage (MVP)

```sh
anjin-intel run \
  --server https://anjin.example.net \
  --token  <enrollment-token-from-the-Intel-tab> \
  --logdir ~/.local/share/Steam/steamapps/compatdata/.../EVE/logs/Chatlogs \
  --channels "Querious.imperium,Delve.imperium"
```

`run` watches the log directory, parses new lines from the allowlisted channels, and
POSTs them in batches. It only ships lines written *after* it starts (no backfill),
and only for channels you list. See [SPEC.md](SPEC.md) for the server contract.

`install` / `uninstall` / `status` (autostart at login via a systemd user unit) land
after the `run` MVP is solid.
