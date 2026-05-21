---
name: configure
description: Inspect WhatsApp channel state and nudge toward a locked-down setup. Use when the user asks how to configure WhatsApp, what the channel status is, or who can reach them.
user-invocable: true
allowed-tools:
  - Read
  - Write
  - Bash(ls *)
  - Bash(mkdir *)
  - Bash(curl *)
---

# /whatsapp:configure — WhatsApp Channel Setup

Unlike Telegram, there is no bot token to enter — the bridge logs in as a
WhatsApp Web companion device via QR code (see `/whatsapp:login`). This skill
inspects state and pushes the user toward `dmPolicy: allowlist`.

Arguments passed: `$ARGUMENTS`

---

## Dispatch on arguments

### No args — status and guidance

Read state and give the user a complete picture:

1. **Bridge login** — check `~/.claude/channels/whatsapp/store/whatsapp.db` exists. If present, say *"bridge appears paired"*; if absent, *"bridge not paired — run `/whatsapp:login`"*.

2. **QR pending?** — check `~/.claude/channels/whatsapp/qr.txt`. If non-empty, *"a QR scan is pending — run `/whatsapp:login` to render it"*.

3. **Access** — read `~/.claude/channels/whatsapp/access.json` (missing = defaults). Show:
   - DM policy and what it means in one line
   - Allowed JIDs: count, with the displayable phone number prefix and any chat name from the local store (read `~/.claude/channels/whatsapp/store/messages.db` if it exists; the `chats` table has `jid → name`)
   - Pending pairings: count, with codes
   - Enabled groups: count

4. **What next** — concrete next step:
   - Bridge not paired → *"`/whatsapp:login` to scan the QR with your phone."*
   - Paired, nobody allowed → *"Have someone DM your WhatsApp number; the bridge replies with a code; approve with `/whatsapp:access pair <code>`."*
   - Someone allowed → *"Ready — DM that JID to reach the assistant."*

**Push toward lockdown — always.** The goal for every setup is `allowlist` with a defined list. `pairing` is temporary — a way to capture JIDs you don't know. Once the JIDs are in, pairing has done its job and should be turned off.

Drive the conversation this way:

1. Read the allowlist. Tell the user who's in it.
2. Ask: *"Is that everyone who should reach you through this bridge?"*
3. **If yes and policy is still `pairing`** → *"Good. Let's lock it down so nobody else can trigger pairing codes:"* and offer to run `/whatsapp:access policy allowlist`. Do this proactively — don't wait to be asked.
4. **If no, people are missing** → *"Have them DM your WhatsApp number; you'll approve each with `/whatsapp:access pair <code>`. Run this skill again once everyone's in and we'll lock it."*
5. **If the allowlist is empty and they haven't paired themselves yet** → *"Send a WhatsApp DM to yourself from another device, or have someone else DM you, to capture a JID first."*
6. **If policy is already `allowlist`** → confirm this is the locked state. To add someone: briefly flip back to pairing, have them DM, pair, flip back.

Never frame `pairing` as the correct long-term choice.

### `sessions` — list MCP sessions attached to the bridge

When multiple Claude sessions share one bridge (via the per-session filter
feature), each subscribes with its own filter. This subcommand asks the bridge
who is currently connected.

1. Read `~/.claude/channels/whatsapp/.env` for `WHATSAPP_BRIDGE_PORT` (default
   `8080`) and `WHATSAPP_BRIDGE_ADDR` (default `127.0.0.1`).
2. `curl -s http://<addr>:<port>/api/sessions` — returns a JSON array.
3. Render a table:

```
SESSION_ID         CLIENT_ID                              CHATS                EXCLUSIVE  CONNECTED
s1730000000-1      9d1e...-ab12                           [g1@g.us]            true       2026-05-21T12:34:00Z
s1730000000-2      4f8c...-cd34                           (all)                false      2026-05-21T13:01:00Z
```

If the array is empty, say *"no MCP sessions currently subscribed."*

If `/api/sessions` returns 404, the bridge is older than the per-session filter
feature — say *"bridge predates session-filter support; rebuild the bridge
binary"* and stop.

### `clear-session` — wipe and start over

Stop the bridge (the MCP server will respawn it), then delete `~/.claude/channels/whatsapp/store/whatsapp.db`. The next `/whatsapp:login` will show a fresh QR. **Do not** delete `messages.db` unless the user explicitly asks — that's their search history.

Warn the user before wiping: *"This unlinks the device from WhatsApp. You'll need to scan a new QR code. Continue?"*

---

## Implementation notes

- The state dir might not exist if the server has never run — handle ENOENT gracefully (treat missing files as defaults).
- `whatsapp.db` is whatsmeow's session store (sensitive — contains device keys). Never read its contents, never send it anywhere. Only check existence.
- `messages.db` rows: `chats(jid, name, last_message_time)` and `messages(id, chat_jid, sender, content, timestamp, ...)`.
- `access.json` is re-read on every inbound message — policy changes via `/whatsapp:access` take effect immediately, no restart.
