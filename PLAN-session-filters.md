# Per-session inbound filter

Routing fan-out from a single bridge to multiple MCP-connected Claude sessions, declared at session launch.

## Goal

Replace the "run a second bridge to scope a Claude session to a different chat"
workaround. WhatsApp caps linked companion devices at ~4; multi-bridging burns
slots fast for what is fundamentally a routing problem, not an isolation one.

After this change: **one device pairing, one bridge, one `access.json` — N MCP
sessions, each declaring a filter at launch, each receiving only the inbound
events its filter matches.**

## Two layers, kept separate

| Layer          | Purpose                                                            | Source of truth                |
|----------------|--------------------------------------------------------------------|--------------------------------|
| `access.json`  | Global gate — is this sender/chat allowed to reach the bot at all? | File (existing)                |
| Session filter | Routing — which connected session handles an allowed message?      | Launch param, in-memory only   |

A chat blocked in `access.json` never reaches any session regardless of filter.
The session filter sits **below** the access gate.

## Filter shape

Env var `WHATSAPP_SESSION_FILTER`, JSON:

```json
{
  "chats": ["120363...@g.us"],
  "excludeChats": [],
  "mentionPatterns": ["^agent\\b"],
  "exclusive": false
}
```

- No env var, or empty → match everything (backwards-compatible with today).
- `mentionPatterns`: if non-nil, **fully overrides** the global
  `access.mentionPatterns` for this session. If absent, the bridge falls
  through to the global setting. (Decision: "session-only if present, else
  global.")
- `exclusive: true`: opt-in; bridge maintains a per-chat claim registry.

## Wire protocol

After WS connect at `/ws/events`:

```
client → bridge:  {"type":"subscribe","filter":{...},"exclusive":bool}
bridge → client:  {"type":"subscribe_ack","ok":true,"session_id":"..."}
                  or {"type":"subscribe_ack","ok":false,"error":"..."}
```

If `ok:false`, the MCP server exits non-zero so the Claude session sees the
failure loudly rather than silently receiving no events.

### Subscribe race handling

A WS connection is created in **pending** state in the hub. `dispatch()` skips
pending clients. On `subscribe_ack`, the client flips to **active** and any
events captured in a small per-client replay buffer (cap ~16) are drained.

This closes the window where an inbound message could arrive between WS
upgrade and the subscribe frame and leak past the routing layer.

## Resolution policy

- **Default**: broadcast to all clients whose filter matches (today's
  behavior, preserved).
- **`exclusive: true`**: bridge registers a claim per chat JID. Second client
  attempting an exclusive claim on the same chat gets `subscribe_ack{ok:false,
  error:"chat X already claimed by session Y"}` and is closed.

### Exclusive-claim reconnect grace (30s)

WS disconnect does **not** immediately release exclusive claims. The bridge
holds them for 30 seconds, keyed by `client_id`. The MCP server persists its
`client_id` to `STATE_DIR/session.id` and reuses it across restarts.

- Reconnect with the same `client_id` within 30s → claim re-honored.
- Reconnect with a different `client_id` within 30s → conflict.
- After 30s with no reconnect → claim freed; next claimant wins.

Without this, a brief network blip (laptop sleep, NAT timeout) lets another
session steal the chat.

## The bridge-ownership problem (P0 — must land first)

```
                              CURRENT STATE
                              ─────────────
   Session 1 starts                   Session 2 starts (later)
        │                                    │
        ▼                                    ▼
   spawn server.ts                      spawn server.ts
        │                                    │
   takeOverPidFile(bridge.pid)          takeOverPidFile(bridge.pid)
        │                                    │
   bridge.pid empty → spawn bridge      bridge.pid set → SIGTERM session 1's bridge!
        │                                    │
   write bridge.pid                     spawn own bridge, write own pid
        │                                    │
   WS connect ─── bridge dies ──────►   bridge respawned, WS connects
                                        Session 1 silently loses its channel.
```

`server.ts:88` (`takeOverPidFile(BRIDGE_PID_FILE)`) makes the brief's "one
bridge, many sessions" premise structurally impossible today. Multi-session
breaks the moment the second `claude` launches.

### Fix: health-probe before spawn

```
                               NEW STATE
                               ─────────
   Session 1 starts                   Session 2 starts (later)
        │                                    │
        ▼                                    ▼
   GET /api/health                      GET /api/health
        │                                    │
   timeout/refused                      200 OK
        │                                    │
   takeOverPidFile(bridge.pid)          (skip takeOver, skip spawn)
   spawn bridge                              │
   write bridge.pid                          │
        │                                    │
   WS connect ─── shared bridge ───►   WS connect (sibling client)
```

- Session 1 owns the bridge child process and the PID file.
- Session 2+ are pure WS clients; they neither spawn nor SIGTERM.
- If session 1 exits, its bridge dies (current behavior preserved). Session 2's
  next inbound attempt fails; it reconnects and on next health probe finds the
  bridge dead → spawns a new one. PID-file recovery still works for crashed
  bridges (probe times out → spawn).

This **must land before** any of the per-session filter work matters.

## Filter evaluation order

```
Inbound message from whatsmeow
        │
        ▼
   server.ts gate() — access.json check (existing, unchanged)
        │
        ▼ (allowed)
   bridge dispatch() per-client loop:
        │
        ├── client A: filter.matches(evt, globalMention)? → send/skip
        ├── client B: filter.matches(evt, globalMention)? → send/skip
        └── client C: pending? → buffer
```

Filter eval lives in the **bridge**, not server.ts. The brief's layering is
preserved: `gate()` stays in server.ts as the access enforcer; routing is
purely bridge-side.

`sessionFilter.matches()` order:
1. `Chats` non-empty AND `evt.ChatJID` not in list → no.
2. `evt.ChatJID` in `ExcludeChats` → no.
3. If a mention check applies (group + `requireMention`), use
   `f.MentionPatterns` if non-nil, else fall through to global. Regex no-match
   → no.
4. Default → yes.

## File-by-file changes

### `bridge/main.go` (~250 LoC)

1. New types near `eventHub` (around line 64):
   ```go
   type sessionFilter struct {
       Chats           []string
       ExcludeChats    []string
       MentionPatterns []string         // nil = inherit global
       Exclusive       bool
       mentionRE       []*regexp.Regexp // precompiled at subscribe time
   }
   type wsClient struct {
       conn      *websocket.Conn
       ch        chan []byte
       filter    *sessionFilter // nil until subscribe
       pending   bool           // true until subscribe_ack sent
       backlog   [][]byte       // cap 16, drained on activation
       id        string         // session_id (uuid)
       clientID  string         // persistent across reconnects (from subscribe frame)
       connected time.Time
   }
   ```
2. Refactor `eventHub`:
   - `map[*websocket.Conn]*wsClient`
   - `broadcast(payload)` → `dispatch(evt InboundEvent, payload []byte, globalMention []string)`
   - Snapshot `(conn, filter, ch, pending)` under lock, evaluate + send **outside** the lock (don't compile regexes inside a hot path; regexes are precompiled at subscribe time anyway).
3. `claimRegistry`:
   - `map[chatJID]claim` where `claim = {clientID, releasedAt time.Time}`
   - `claim(jid, clientID) error` — succeeds if free, or if `releasedAt > 0` and `clientID` matches; conflict otherwise.
   - `release(clientID)` on disconnect sets `releasedAt = now` for all chats claimed by that client; does **not** delete.
   - Background goroutine (or lazy check) deletes claims where `releasedAt + 30s < now`.
4. `/ws/events` handler:
   - Add client to hub in pending state.
   - Set read deadline ~5s for the first frame.
   - Decode subscribe frame; validate JID shape on every chat; precompile mention regexes.
   - On `exclusive:true`, attempt claims; any conflict → `subscribe_ack{ok:false}`, close.
   - Send `subscribe_ack{ok:true, session_id}`, flip pending=false, drain backlog.
5. `GET /api/sessions` (read-only):
   ```json
   [{"session_id":"...","client_id":"...","filter":{...},"exclusive":true,"connected_at":"..."}]
   ```
6. Mention-regex parity comment: short block above `sessionFilter.matches()`
   noting the regex semantics must match `server.ts isMentioned()` —
   anchored, case-insensitive, word-boundary behavior identical.

**RE2 note**: Go's `regexp` is RE2-based, so catastrophic backtracking is not
possible. No need to validate regex complexity; one-line code comment to that
effect.

### `server.ts` (~110 LoC)

1. **Bridge spawn-skip** (new code around line 220):
   ```ts
   async function ensureBridge(): Promise<void> {
     try {
       const r = await fetch(`${BRIDGE_BASE}/api/health`, { signal: AbortSignal.timeout(1000) })
       if (r.ok) {
         process.stderr.write('whatsapp channel: attaching to existing bridge\n')
         return // don't takeOver PID, don't spawn
       }
     } catch {}
     takeOverPidFile(BRIDGE_PID_FILE)
     spawnBridge()
     await waitForBridgeReady()
   }
   ```
   Replace the current `takeOverPidFile(BRIDGE_PID_FILE)` at line 88 + the
   `spawnBridge()` call at line 779 with this guarded path.
2. **Persistent `client_id`**: read/write `STATE_DIR/session.id` (uuid v4 if
   absent). Pass in subscribe frame.
3. **Filter env parsing**: `parseFilterEnv()` at startup. Schema-validate
   (typed fields, JID shape regex). On error, write a friendly diagnostic to
   stderr and `process.exit(1)`.
4. **Add to bridge-spawn env** (line 229-234): only `WHATSAPP_SESSION_FILTER`
   passed through if the user set it — but this is for the *MCP server's own*
   use, not the bridge's; the bridge learns the filter via the subscribe
   frame. Keep this env on the MCP-server side only.
5. **WS connect**: on `open`, send subscribe frame; await `subscribe_ack`. If
   `ok:false`, log error and `process.exit(1)`.
6. `gate()` at line 599: **unchanged**. Access enforcement stays where it is.

### `skills/access/` (~15 LoC)

`WHATSAPP_ACCESS_MODE=static` is **already implemented** in server.ts
(line 69, 165, 182, 664). The skill just needs to honor it.

In every mutating action (`add`, `remove`, `pair`, etc.): early-check
`process.env.WHATSAPP_ACCESS_MODE === 'static'` and refuse:

```
Access is in static mode (WHATSAPP_ACCESS_MODE=static). Mutations are
disabled. Edit ~/.claude/channels/whatsapp/access.json directly, then
restart the bridge.
```

No server.ts changes. No bridge changes.

### `skills/configure/` (~80 LoC, new sub-route)

Read-only view: `GET /api/sessions`, render a table:

```
SESSION_ID  CLIENT_ID  CHATS              EXCLUSIVE  CONNECTED
session-a   abc123     [g1@g.us]          true       2026-05-21 12:34
session-b   def456     [g2@g.us, dm@..]   false      2026-05-21 13:01
```

Pure inspection, no mutations.

### Docs (`README.md`, `ACCESS.md`)

New section: "Per-session filters."
- Env var schema with examples.
- Broadcast vs exclusive semantics.
- Security posture: `WHATSAPP_SESSION_FILTER` + `WHATSAPP_ACCESS_MODE=static`
  = launch-time-immutable routing, useful for shared-context/automated
  sessions where an attacker controlling the prompt should not be able to
  expand reach.

## Tests

Repo has no Go test files today. Adding `bridge/main_test.go` (and split files
as needed). No JS test harness exists; tests on the server.ts side are
documented as manual smoke until a test framework lands (out of scope).

### Unit (Go) — required
- `sessionFilter.matches`: table-driven, 8 cases (see test diagram below).
- `claimRegistry`: 7 cases including grace-window reclaim.
- `subscribe` frame parsing: malformed, invalid JID, valid.

### Regression (mandatory) — Go
- **Bridge-spawn skip with live bridge** — second `ensureBridge()` invocation does **not** call `takeOverPidFile` or `spawnBridge`.
- **Bridge-spawn fallback with dead bridge** — health probe times out → both run normally.

Iron rule: both are P0. Without them, future refactors of the spawn path can
silently reintroduce the bug Issue 1 fixes.

### Manual smoke (documented in CHANGELOG)
- Two `server.ts` instances against one bridge, different filters → routing
  works as documented.
- Single-session with no filter → behaves exactly as today.
- Exclusive conflict → second session exits with clear error.

### Coverage map

```
[+] bridge/main.go
    ├── sessionFilter.matches()
    │   ├── empty filter → match all                          [test]
    │   ├── Chats hit / miss                                  [test]
    │   ├── ExcludeChats overrides allowlist                  [test]
    │   ├── MentionPatterns nil → falls through to global     [test]
    │   ├── MentionPatterns set → ignores global              [test]
    │   ├── Group + requireMention + no match → drop          [test]
    │   └── DM (no mention check)                             [test]
    │
    ├── claimRegistry
    │   ├── First claim succeeds                              [test]
    │   ├── Second exclusive on same chat → conflict          [test]
    │   ├── Non-exclusive + non-exclusive both succeed        [test]
    │   ├── Non-exclusive + later exclusive → conflict        [test]
    │   ├── Release on disconnect (grace 30s)                 [test]
    │   ├── Grace: same client_id within 30s → reclaim        [test, REGRESSION-RISK]
    │   └── Grace: different client_id within 30s → conflict  [test]
    │
    ├── /ws/events handler
    │   ├── Subscribe deadline (5s) → close                   [test]
    │   ├── Malformed subscribe JSON → ack{ok:false}          [test]
    │   ├── Invalid JID in filter → ack{ok:false}             [test]
    │   └── Pending-state buffer drain on activation          [test]
    │
    └── /api/sessions returns subscribed clients              [test]

[+] server.ts (manual smoke only — no JS harness)
    ├── ensureBridge: probe alive → skip spawn + skip PID     [smoke, REGRESSION-CRITICAL]
    ├── ensureBridge: probe dead → spawn + take PID           [smoke, REGRESSION-CRITICAL]
    ├── parseFilterEnv: empty / valid / malformed             [smoke]
    └── Persistent client_id (write once, reuse)              [smoke]

[+] skills/access static-mode guard                            [smoke]
[+] User flows (two-session smoke, exclusive conflict)         [smoke, E2E manual]

─────────────────────────────────
GO COVERAGE: 21/21 paths covered by unit tests
JS COVERAGE: 0/4 paths (manual smoke; no harness)
REGRESSIONS: 2 critical (server.ts spawn skip) + 1 risk (claim grace)
─────────────────────────────────
```

## Failure modes

| Codepath              | Failure                                           | Coverage                                                    |
|-----------------------|---------------------------------------------------|-------------------------------------------------------------|
| Bridge-spawn skip     | Probe 200 but bridge crashes 1ms later            | WS connect fails → server.ts exit(1). Loud.                 |
| Exclusive claim       | Session dies without TCP FIN (`kill -9`)          | 30s grace window covers most of it; eventually freed.       |
| Subscribe ack         | Bridge segfaults between ack and dispatch         | WS read fails → server.ts exit(1). Loud.                    |
| User regex            | Catastrophic backtracking in `mentionPatterns`    | Impossible — Go RE2. Comment in code explains why.          |
| Two MCP servers race  | Both pass health probe simultaneously, both spawn | Second `spawnBridge` fails to bind port → exit. Acceptable. |

## NOT in scope (deferred)

- **Standalone bridge daemon** (systemd/launchctl). Cleaner long-term
  ownership model; ships an install step. Defer.
- **JS test harness** (Jest/Vitest). Repo has none; not adding in this PR.
- **Per-session access policy.** Brief excludes; access stays global.
- **`/api/sessions` write endpoints** (kick session, force-release claim).
  Read-only is enough for v1.
- **Filter hot-reload.** Change requires session restart.

## What already exists (reused, not rebuilt)

- `STATIC` mode in `server.ts` — fully wired; access skill just needs to honor it.
- `takeOverPidFile()` + `bridge.pid` lifecycle — current spawn path; `ensureBridge` wraps it.
- `waitForBridgeReady()` — already pings `/api/health`; spawn-skip reuses it.
- `gate()` access check — stays exactly as-is.
- `eventHub` fanout core — refactored, not rewritten.

## Parallelization

| Lane | Steps                                                              | Modules touched         |
|------|--------------------------------------------------------------------|-------------------------|
| A    | `ensureBridge` spawn-skip + regression tests → subscribe-frame send | `server.ts`             |
| B    | Bridge filter + claim registry + `/api/sessions` + Go unit tests   | `bridge/main.go`        |
| C    | `/whatsapp:access` static-mode guard                                | `skills/access/`        |

**Order**: A and B in parallel worktrees (different files). C independent;
land anytime. After A+B+C merged: `/whatsapp:configure` read-only view +
docs.

**Conflict risk**: low. A and B touch disjoint files; C is isolated.

## Decisions locked (from review)

| # | Decision                                                      | Source                |
|---|---------------------------------------------------------------|-----------------------|
| 1 | Health-probe before spawn (skip if alive)                     | Eng review Issue 1A   |
| 2 | Pending-state buffer for subscribe race                       | Eng review Issue 2A   |
| 3 | 30s exclusive-claim grace + persistent `client_id`            | Eng review Issue 3A   |
| 4 | Two parallel mention-regex impls, kept in sync via tests      | Eng review Issue 4    |
| 5 | Validate filter at both ends (server.ts + bridge)             | Eng review Issue 5    |
| 6 | Precompile regexes at subscribe; release lock before send     | Eng review Issue 6    |
| 7 | Mention precedence: session-only if present, else global      | User answer           |

## Estimate

~330 LoC Go, ~110 LoC TS, ~15 LoC skill, ~80 LoC configure skill, ~50 LoC
docs, ~250 LoC Go tests. One PR.

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | — |
| Codex Review | `/codex review` | Independent 2nd opinion | 0 | — | — |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | CLEAR (PLAN) | 1 P0, 2 P1/P2 architecture; 2 code-quality; 28 test gaps mapped; 1 perf; all resolved |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | — |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | — |

**VERDICT:** ENG CLEARED — ready to implement.
