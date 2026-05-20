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

**Critical:** WhatsApp's pairing QR rotates every ~30 seconds. A stale QR (>30s old) will silently fail to link — the phone scans and nothing happens. So *first* verify the bridge is alive and the QR is fresh, *then* render it.

1. **Verify the bridge is up.** Bash: `curl -s --max-time 2 http://127.0.0.1:8080/api/health`. Expect a JSON body like `{"connected":true,"logged_in":false}`. If the curl fails or returns non-zero, the bridge isn't running — tell the user: *"The bridge isn't running. Make sure Claude Code is launched with `--channels plugin:whatsapp@claude-whatsapp` and that `bun` is in PATH. Check `claude --debug` output for spawn errors."* Stop here.
2. **Verify the QR is fresh.** Read `~/.claude/channels/whatsapp/qr.txt`.
   - **If missing** → bridge is up but no QR pending. Either it's already paired (check `ls -la ~/.claude/channels/whatsapp/store/whatsapp.db`) or the bridge gave up waiting (3-minute timeout). Tell the user: *"No QR pending. If you expected one, restart the channel-mode session: exit Claude, then `claude --channels plugin:whatsapp@claude-whatsapp`."*
   - **If present but `stat -c %Y qr.txt` shows it's more than ~25 seconds old** → it's likely stale. Tell the user: *"That QR is N seconds old; WhatsApp will probably reject it. Restart the channel session to force a fresh one."* Don't bother rendering.
   - **If present and fresh (<25s)** → continue.
3. Try `qrencode -t UTF8 -m 1 < ~/.claude/channels/whatsapp/qr.txt` via Bash. If that succeeds, print the QR.
4. If `qrencode` isn't installed, fall back: tell the user *"`qrencode` not installed (`apt install qrencode` / `brew install qrencode`). Run `tail -n 50 ~/.claude/channels/whatsapp/bridge.log` — the bridge printed the QR there in ASCII art. Make sure your terminal is at least ~70 cols wide."*
5. Tell the user *"Scan within ~25 seconds. If it doesn't link, the QR likely rotated; re-run this skill. Once paired, qr.txt disappears and the session lasts ~20 days."*

### `tail` — show recent bridge log

`tail -n 40 ~/.claude/channels/whatsapp/bridge.log`. Useful to see whether pairing succeeded.

### `reset` — wipe the WhatsApp session

Same as `/whatsapp:configure clear-session`. Confirm with the user first — this unlinks the device from WhatsApp.

---

## Implementation notes

- The QR string changes every ~30 seconds; the bridge overwrites `qr.txt` as new codes arrive. If the displayed QR was already stale, just re-run the skill.
- Never log the contents of `~/.claude/channels/whatsapp/store/whatsapp.db` — it contains private device keys.
- If the user wants to pair a *second* device (multi-device), they should run a *separate* MCP instance with `WHATSAPP_STATE_DIR` pointing to a different directory.
