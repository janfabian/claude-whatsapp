---
name: login
description: Render the WhatsApp pairing QR code in this session so the user can scan it with their phone. Use when the user asks to log in, pair a phone, scan a QR, or set up the bridge for the first time.
user-invocable: true
allowed-tools:
  - Read
  - Bash(cat *)
  - Bash(rm *)
  - Bash(qrencode *)
---

# /whatsapp:login — Pair the WhatsApp bridge

The bridge logs in as a WhatsApp Web companion device. On first run (and after the ~20-day session expiry) it writes the pairing string to `~/.claude/channels/whatsapp/qr.txt`. This skill renders it as a QR code in your terminal.

Arguments passed: `$ARGUMENTS`

---

## Dispatch

### No args — show the QR

1. Read `~/.claude/channels/whatsapp/qr.txt`.
   - **If missing** → either the bridge isn't running or it's already paired. Tell the user: *"No QR pending. If you expected one, check that Claude Code was launched with `--channels plugin:whatsapp@<source>` and that the bridge is up — see `~/.claude/channels/whatsapp/bridge.log`."*
   - **If present** → continue.
2. Try `qrencode -t UTF8 -m 1 < ~/.claude/channels/whatsapp/qr.txt` via Bash. If that succeeds, print the QR.
3. If `qrencode` isn't installed, fall back: show the raw pairing string and tell the user *"`qrencode` not installed (`apt install qrencode` / `brew install qrencode`). Or open `~/.claude/channels/whatsapp/bridge.log` — the bridge already printed the QR there in ASCII art."*
4. After scanning succeeds, the bridge deletes `qr.txt`. Tell the user *"Once you've scanned, the QR file disappears and the bridge is paired. The session lasts ~20 days, after which you'll need to re-run this."*

### `tail` — show recent bridge log

`tail -n 40 ~/.claude/channels/whatsapp/bridge.log`. Useful to see whether pairing succeeded.

### `reset` — wipe the WhatsApp session

Same as `/whatsapp:configure clear-session`. Confirm with the user first — this unlinks the device from WhatsApp.

---

## Implementation notes

- The QR string changes every ~30 seconds; the bridge overwrites `qr.txt` as new codes arrive. If the displayed QR was already stale, just re-run the skill.
- Never log the contents of `~/.claude/channels/whatsapp/store/whatsapp.db` — it contains private device keys.
- If the user wants to pair a *second* device (multi-device), they should run a *separate* MCP instance with `WHATSAPP_STATE_DIR` pointing to a different directory.
