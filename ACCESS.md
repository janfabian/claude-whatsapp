# WhatsApp — Access & Delivery

A WhatsApp account paired through this plugin can receive messages from anyone who knows your phone number. The access model decides who actually reaches the assistant.

By default, a DM from an unknown sender triggers **pairing**: the bridge replies with a 6-character code and drops the message. You run `/whatsapp:access pair <code>` from your assistant session to approve them. Once approved, their messages pass through.

All state lives in `~/.claude/channels/whatsapp/access.json`. The `/whatsapp:access` skill commands edit this file; the server re-reads it on every inbound message, so changes take effect without a restart. Set `WHATSAPP_ACCESS_MODE=static` to pin config to what was on disk at boot (pairing is unavailable in static mode since it requires runtime writes).

## At a glance

| | |
| --- | --- |
| Default policy | `pairing` |
| Sender ID | WhatsApp JID (e.g. `1234567890@s.whatsapp.net`) |
| Group key | Group JID, ends in `@g.us` |
| Reactions | Any emoji (no fixed whitelist, unlike Telegram) |
| Config file | `~/.claude/channels/whatsapp/access.json` |

## DM policies

`dmPolicy` controls how DMs from senders not on the allowlist are handled.

| Policy | Behavior |
| --- | --- |
| `pairing` (default) | Reply with a pairing code, drop the message. Approve with `/whatsapp:access pair <code>`. |
| `allowlist` | Drop silently. No reply. Use this once you have everyone's JID captured. |
| `disabled` | Drop everything, including allowlisted users and groups. Useful for temporary mute. |

```
/whatsapp:access policy allowlist
```

## JIDs

WhatsApp identifies users by their **phone number + `@s.whatsapp.net`** suffix. Some carriers/devices add a device index: `1234567890:5@s.whatsapp.net`. Both forms are valid. Pairing captures the exact JID as the sender presented it.

```
/whatsapp:access allow 1234567890@s.whatsapp.net
/whatsapp:access remove 1234567890@s.whatsapp.net
```

## Groups

Groups are off by default. Opt each one in individually.

```
/whatsapp:access group add 1234567890-1623456789@g.us
```

Group JIDs are visible in the `<channel>` meta whenever a group message is dropped (check `bridge.log` for recent group activity) — or look in `~/.claude/channels/whatsapp/store/messages.db` (`SELECT jid, name FROM chats WHERE jid LIKE '%@g.us'`).

With the default `requireMention: true`, the bridge delivers only when the message matches one of `mentionPatterns`. Pass `--no-mention` to process every message, or `--allow jid1,jid2` to restrict which members can trigger Claude.

```
/whatsapp:access group add 1234567890-1623456789@g.us --no-mention
/whatsapp:access group add 1234567890-1623456789@g.us --allow 9876543210@s.whatsapp.net
/whatsapp:access group rm 1234567890-1623456789@g.us
```

## Mention detection

WhatsApp doesn't have a native "mention this bot" concept (the assistant is invisible to other group members — messages flow through your own account). So the bridge matches against `mentionPatterns` instead:

```
/whatsapp:access set mentionPatterns '["^hey claude\\b", "\\bassistant\\b"]'
```

A reply to one of *your own* messages also delivers, regardless of patterns — that's the most natural way to "address the assistant" in a group.

## Delivery

Configure outbound behavior with `/whatsapp:access set <key> <value>`.

**`ackReaction`** reacts to inbound messages on receipt. WhatsApp accepts any single emoji.

```
/whatsapp:access set ackReaction 👀
/whatsapp:access set ackReaction ""
```

**`textChunkLimit`** sets the split threshold (default 4096, which is also WhatsApp's effective cap).

**`chunkMode`** chooses the split strategy: `length` cuts exactly at the limit; `newline` prefers paragraph boundaries.

## Skill reference

| Command | Effect |
| --- | --- |
| `/whatsapp:access` | Print current state: policy, allowlist, pending pairings, enabled groups. |
| `/whatsapp:access pair a4f91c` | Approve pairing code. Adds the sender to `allowFrom`; the bridge sends "Paired!" on WhatsApp. |
| `/whatsapp:access deny a4f91c` | Discard a pending code. The sender is not notified. |
| `/whatsapp:access allow <jid>` | Add a JID directly. |
| `/whatsapp:access remove <jid>` | Remove from the allowlist. |
| `/whatsapp:access policy allowlist` | Set `dmPolicy`. Values: `pairing`, `allowlist`, `disabled`. |
| `/whatsapp:access group add <jid>` | Enable a group. Flags: `--no-mention`, `--allow jid1,jid2`. |
| `/whatsapp:access group rm <jid>` | Disable a group. |
| `/whatsapp:access set ackReaction 👀` | Set a config key: `ackReaction`, `textChunkLimit`, `chunkMode`, `mentionPatterns`. |

## Config file

`~/.claude/channels/whatsapp/access.json`. Absent file is equivalent to `pairing` policy with empty lists, so the first DM triggers pairing.

```jsonc
{
  // Handling for DMs from senders not in allowFrom.
  "dmPolicy": "pairing",

  // WhatsApp JIDs allowed to DM.
  "allowFrom": ["1234567890@s.whatsapp.net"],

  // Groups the bridge is active in. Empty object = DM-only.
  "groups": {
    "1234567890-1623456789@g.us": {
      // true: respond only when content matches mentionPatterns
      // (WhatsApp has no native mention-detection from a non-participant POV).
      "requireMention": true,
      // Restrict triggers to these senders. Empty = any member (subject to requireMention).
      "allowFrom": []
    }
  },

  // Case-insensitive regexes that count as a mention.
  "mentionPatterns": ["^hey claude\\b"],

  // Emoji to react with on receipt. Empty string disables.
  "ackReaction": "👀",

  // Split threshold for long replies.
  "textChunkLimit": 4096,

  // length = cut at limit. newline = prefer paragraph boundaries.
  "chunkMode": "newline"
}
```

## Permission relay

When Claude requests permission for a sensitive tool call, the channel forwards the prompt to every JID on `allowFrom`. Reply with `y <request-id>` to allow or `n <request-id>` to deny — the request ID is a 5-character code (a-z minus `l`), shown in Claude's prompt and in the message sent to your phone. Groups are intentionally **not** included in permission relays — only senders who passed explicit pairing.
