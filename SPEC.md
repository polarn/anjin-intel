# anjin-intel ⇄ anjin: wire contract

The shipper has **zero code dependency** on the anjin server. Its entire contract
is this small, versioned HTTP shape. `protocolVersion` lets the server return a
graceful "please update your shipper" rather than break silently on skew.

The server URL + enrollment token are supplied by the user at install time
(copy-pasted from the anjin Intel tab) — never baked into the binary.

## Ingest

```
POST {server}/api/intel
Authorization: Bearer <enrollment-token>
Content-Type: application/json

{
  "protocolVersion": 1,
  "lines": [
    { "channel": "Querious.imperium",
      "ts": "2026-06-23T19:04:11Z",   // RFC3339, UTC (EVE time)
      "sender": "Some Pilot",
      "message": "neut in FD-MLJ" }
  ]
}
```

Responses:
- `200` — accepted. Body: `{ "received": N, "accepted": M }` (accepted ≤ received;
  the server drops lines for channels not on the user's allowlist).
- `401` — missing/invalid/revoked token.
- `409` — protocol mismatch. Body includes the server's supported `protocolVersion`;
  the shipper should warn the user to update.

The shipper only sends lines for channels on its **allowlist**; the server *also*
enforces the allowlist (defense in depth). Default allowlist is empty — nothing is
shipped until the user opts a channel in.

## Channel allowlist (server-authoritative; not yet consumed by the shipper)

```
GET {server}/api/intel/config
Authorization: Bearer <enrollment-token>
→ { "channels": ["Querious.imperium", "Delve.imperium", ...] }
```

The shipper polls this periodically and tails only listed channels. `--channels a,b,c`
at install/run time seeds a local allowlist for offline/simple use; the server list
(when reachable) is authoritative. *(Server-poll integration lands after the MVP; the
MVP uses the local `--channels` seed.)*

## Seen-channel discovery (privacy-safe; not yet consumed by the shipper)

```
POST {server}/api/intel/channels
Authorization: Bearer <enrollment-token>
{ "seen": ["Local", "Querious.imperium", "Corp", ...] }
```

Channel **names only** (no message content), so the Intel tab can offer a picker.

## Protocol versions

- **1** — initial: ingest envelope above.
