# WhatsApp

Connect a WhatsApp account to your Claude Code with an MCP server.

The MCP server logs into WhatsApp as a linked companion device (via [whatsmeow](https://github.com/tulir/whatsmeow), an unofficial WhatsApp Web client) and provides tools to Claude to reply, react, mark messages read, and search recent history. When someone messages you (or a configured group mentions Claude), the server forwards the message to your Claude Code session.

> Inspired by — and modelled on — the official [Telegram plugin](https://github.com/anthropics/claude-plugins-official/tree/main/external_plugins/telegram). The Go bridge is adapted from [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) (MIT).

## ⚠️ Caveats

- **Unofficial client.** whatsmeow is reverse-engineered. WhatsApp may flag or ban accounts that use it. Use a secondary number if that risk matters.
- **Re-auth every ~20 days.** WhatsApp expires linked-device sessions. You'll need to re-scan the QR when that happens.
- **Bridge process must stay running.** The MCP server spawns and supervises it; if Claude Code exits, so does the bridge.
- **Anyone with access to an allowlisted chat can drive Claude.** Lock down with `dmPolicy: allowlist`. Treat the pairing window as you would handing someone your terminal.
- **Lethal trifecta.** Claude can `search_history` *and* `send_message` — combined with reading your codebase, that's exfiltration-class exposure. Be deliberate about who's on the allowlist.

## Prerequisites

- [Bun](https://bun.sh) — the MCP server runs on Bun. Install with `curl -fsSL https://bun.sh/install | bash`.
- [Go](https://go.dev/dl/) — to build the whatsmeow bridge binary. Only needed at install time; the binary is cached in `bridge/bin/`.
- (optional) [`qrencode`](https://fukuchi.org/works/qrencode/) — for in-terminal QR rendering by `/whatsapp:login` (`apt install qrencode` / `brew install qrencode`). Without it, you can still scan from the bridge's stdout log.
- A spare phone with WhatsApp installed for the linked-device QR scan.

## Quick setup

**1. Install the plugin.**

These are Claude Code commands — run `claude` to start a session first.

```
/plugin marketplace add janfabian/claude-whatsapp
/plugin install whatsapp@claude-whatsapp
/reload-plugins
```

The first launch runs `bun install` and compiles the bridge binary (requires Go). Subsequent launches reuse it.

**2. Relaunch with the channel flag.**

The server won't ingest messages without this — exit your session and start a new one:

```sh
claude --channels plugin:whatsapp@claude-whatsapp
```

**3. Scan the QR.**

The bridge writes a QR pairing string to `~/.claude/channels/whatsapp/qr.txt` on first run. From your Claude Code session:

```
/whatsapp:login
```

Scan the QR with WhatsApp → Settings → Linked Devices → Link a Device. Once accepted, the file disappears and the bridge is paired.

**4. Pair a sender.**

Have someone DM your number (or DM it from another device). The bridge replies with a 6-character code and drops the message. In Claude Code:

```
/whatsapp:access pair <code>
```

Their next DM reaches the assistant.

**5. Lock it down.**

Pairing is for capturing JIDs. Once you're in, switch to `allowlist` so strangers don't get pairing-code replies. Ask Claude to do it, or run `/whatsapp:access policy allowlist` directly.

## Access control

See **[ACCESS.md](./ACCESS.md)** for DM policies, groups, mention detection, delivery config, skill commands, and the `access.json` schema.

Quick reference: JIDs look like `1234567890@s.whatsapp.net` (DMs) or `…@g.us` (groups). Default policy is `pairing`. Access state lives at `~/.claude/channels/whatsapp/access.json`.

## Tools exposed to the assistant

| Tool | Purpose |
| --- | --- |
| `send_message` | Send to a chat. `chat_id` (JID) + `text`, optionally `files` (absolute paths). Auto-chunks at 4096 chars; files send as separate messages after the text. |
| `react` | Add/remove an emoji reaction. WhatsApp accepts any emoji. Pass `sender_jid` from the inbound meta for group reactions. |
| `mark_read` | Send read receipts for a list of message IDs in a chat. |
| `download_attachment` | Fetch a non-image attachment from a message to the local inbox. Returns the local file path. |
| `search_history` | Substring search the local message store. Useful when the sender alludes to earlier context. WhatsApp-only — Telegram has no equivalent. |

## Media

Inbound **images** are eagerly downloaded to `~/.claude/channels/whatsapp/inbox/` and the path is included as `image_path` in the `<channel>` notification so the assistant can `Read` it directly.

Other media (video, audio, documents, voice notes) are surfaced as `attachment_kind` + `attachment_message_id` in the meta; the assistant calls `download_attachment` to fetch them on demand.

## Why no edit / typing indicator?

WhatsApp's Web API exposes neither. Long-running tasks should send a *new* message when done — that way the user's phone pings them. WhatsApp does support reactions, though, so the channel applies one (configurable via `ackReaction`) on receipt as a quiet ack.

## State directory

```
~/.claude/channels/whatsapp/
├── access.json              # who can reach you (managed by /whatsapp:access)
├── inbox/                   # downloaded media (safe to delete)
├── store/                   # whatsmeow session + searchable history SQLite
├── qr.txt                   # pairing QR string (only present pre-login)
├── bridge.log               # bridge stdout+stderr
├── bridge.pid / mcp.pid     # process bookkeeping
└── .env                     # optional WHATSAPP_BRIDGE_PORT etc
```

To run multiple bridges on one machine (different accounts), point `WHATSAPP_STATE_DIR` at a different directory and use a different `WHATSAPP_BRIDGE_PORT` per instance.

## Troubleshooting

- **No QR appears.** Check `bridge.log`. If the bridge can't start, the binary may be missing — run `bash scripts/build-bridge.sh` (requires Go).
- **"chat is not allowlisted" when Claude tries to reply.** Outbound tools enforce the same allowlist as inbound. Add the JID via `/whatsapp:access allow <jid>` or `group add <jid>`.
- **Sender DMs and gets nothing back.** Check `dmPolicy`. If it's `allowlist`, unknown senders are dropped silently. Flip to `pairing` briefly to capture their JID.
- **Stuck after re-launch.** A previous bridge may still hold the WhatsApp socket. Look for stale processes with the PID files in the state dir.
- **Re-auth needed.** Delete `~/.claude/channels/whatsapp/store/whatsapp.db` and `/whatsapp:login` again. `messages.db` (your search history) survives.
