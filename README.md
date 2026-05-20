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

- [Bun](https://bun.sh) — the MCP server runs on Bun. Install with `curl -fsSL https://bun.sh/install | bash`. **Make sure `~/.bun/bin` is in the PATH of the shell that launches `claude`**, otherwise the MCP server spawn fails silently.
- [Go](https://go.dev/dl/) 1.23+ — to build the whatsmeow bridge binary. Only needed at install time; the binary is cached in `bridge/bin/`. Needs CGO (Linux/macOS work out of the box; Windows needs a C toolchain like MSYS2).
- (optional) [`qrencode`](https://fukuchi.org/works/qrencode/) — for in-terminal QR rendering by `/whatsapp:login` (`apt install qrencode` / `brew install qrencode`). Without it, the bridge's `bridge.log` already contains an ASCII QR you can scan — see step 3 below.
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

The bridge writes the pairing string to `~/.claude/channels/whatsapp/qr.txt` *and* renders the QR as ASCII half-block art to its stdout. Two ways to actually see something scannable:

- **With `qrencode` installed:** from your Claude Code session, `/whatsapp:login` renders an inline UTF-8 QR.
- **Without `qrencode`:** the bridge already drew the QR in its log. Open a terminal large enough to fit a 33×33 block grid (~70 columns wide), then:
  ```sh
  tail -n 50 ~/.claude/channels/whatsapp/bridge.log
  ```
  Scan with WhatsApp → Settings → Linked Devices → Link a Device.

> ⚠️ The raw string in `qr.txt` looks like `2@xVTVC…,Ql31…,I+fX…,0Q4A…,9` — that's pairing data, **not a URL**. Some assistants hallucinate a `https://wa.me/settings/linked_devices#…` prefix; ignore it. The string is only meaningful when encoded as a QR image.
>
> ⚠️ **QRs rotate every ~30 seconds.** Scan immediately. If the link fails, the QR is most likely already stale — `/whatsapp:login` checks `qr.txt`'s mtime and the bridge's health endpoint before rendering, so re-run it to force a fresh check. A stale QR silently fails on the phone with no error message.

Once accepted, `qr.txt` disappears and the bridge is paired. Session lasts ~20 days.

### Verifying the bridge is actually alive

The plugin's MCP server spawns the Go bridge as a child process. If the spawn failed (bun/go not in PATH, port conflict, etc.) `/whatsapp:login` will show a QR that nothing is listening to — and WhatsApp can't link to a dead bridge. Quick sanity check from any terminal:

```sh
pgrep -af whatsapp-bridge                                # should print the pid + command
curl -s http://127.0.0.1:8080/api/health                 # should print {"connected":true,"logged_in":<bool>}
```

If either is empty/fails, the bridge isn't running. Restart your Claude Code session with `--channels plugin:whatsapp@claude-whatsapp` and check `claude --debug` for spawn errors.

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

## Linked-device name

By default the bridge identifies itself to WhatsApp as **"Claude Code"** with a desktop icon — that's what appears on your phone in Settings → Linked Devices. Override via env:

```sh
WHATSAPP_DEVICE_NAME="My Assistant" claude --channels plugin:whatsapp@claude-whatsapp
```

The name is only sent at pairing time. If you're already linked (and seeing the old `whatsmeow` label, or want to change to a new label), you need to re-pair:

1. On your phone: WhatsApp → Settings → Linked Devices → tap the entry → Log Out
2. On disk: `rm ~/.claude/channels/whatsapp/store/whatsapp.db`
3. Relaunch the channel session and `/whatsapp:login` again. `messages.db` (your search history) survives.

## Troubleshooting

- **MCP server didn't start at all.** Most often `bun` isn't in the PATH of the shell that launched `claude`. Verify with `which bun` from the same shell; ensure `~/.bun/bin` is in your shell rc. Same applies to `go` for the bridge build step.
- **No QR appears.** Check `bridge.log`. If the bridge can't start, the binary may be missing — run `bash scripts/build-bridge.sh` from the plugin install dir (see "Plugin install location" below).
- **`bridge.log` shows `client outdated (405)`.** WhatsApp periodically invalidates older whatsmeow clients. Bump and rebuild:
  ```sh
  cd <plugin-install-dir>/bridge
  go get -u go.mau.fi/whatsmeow@latest && go mod tidy
  bash ../scripts/build-bridge.sh
  ```
  Then restart Claude Code. This is *different from* the ~20-day session re-auth.
- **"chat is not allowlisted" when Claude tries to reply.** Outbound tools enforce the same allowlist as inbound. Add the JID via `/whatsapp:access allow <jid>` or `group add <jid>`.
- **Sender DMs and gets nothing back.** Check `dmPolicy`. If it's `allowlist`, unknown senders are dropped silently. Flip to `pairing` briefly to capture their JID.
- **Stuck after re-launch.** A previous bridge may still hold the WhatsApp socket. The MCP server kills stale poller PIDs on startup, but if the process tree got severed (SIGKILL, terminal closed mid-session), look for an orphan `whatsapp-bridge` process and kill it manually.
- **Session expired / re-auth needed.** Delete `~/.claude/channels/whatsapp/store/whatsapp.db` and `/whatsapp:login` again. `messages.db` (your search history) survives.
- **`/whatsapp:login` says "no QR pending".** Either the bridge isn't running (check `bridge.log`), or it's already paired. `ls ~/.claude/channels/whatsapp/store/whatsapp.db` — if it exists with non-zero size, you're already paired.
- **Scanned the QR but my phone won't link.** Almost always a stale QR — WhatsApp's pairing code rotates every ~30s. If you saw the QR more than 25 seconds before scanning, it's expired. Less commonly: the bridge wasn't actually running when you scanned (verify with `pgrep -af whatsapp-bridge`), or you've hit WhatsApp's 4-linked-device limit (remove one from Settings → Linked Devices on the phone).
- **Suspicious / stale state from a previous run.** If something feels off, nuke `~/.claude/channels/whatsapp/` entirely and restart. `messages.db` (your local history) is the only thing you'd want to keep — back it up first if you've got hours of conversation indexed.

## Plugin install location

When installed via `/plugin install whatsapp@claude-whatsapp`, the plugin source lives at:

```
~/.claude/plugins/cache/claude-whatsapp/whatsapp/<version>/
```

That's where `bridge/bin/` is created, where you'd run `git pull` to update manually, and where the build script needs to be run from if the auto-build fails. Use `claude --debug` to see the actual spawn command and capture stderr from the MCP server.
