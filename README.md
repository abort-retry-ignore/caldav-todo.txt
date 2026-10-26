# tuxedodav

A `tuxedo` CLI shim backed by **CalDAV (VTODO)**: keeps the omarchy
[Tuxedo Todo Panel](https://github.com/lpanebr/omarchy-tuxedo-todo) widget
reading/writing a Nextcloud task list instead of a bare todo.txt file.

## How it fits together

```
omarchy widget (stock QML, one-string patch)
   │  reads ~/todo.txt + done.txt (file-watcher picks up external edits)
   ▼
~/todo.txt  ◀── daemon (systemd user unit, pulls every 60s)
   ▲  add / do n / replace n / del n
   └── tuxedodav ──CalDAV──▶ Nextcloud (cloud.021407.xyz) ⇄ Reminders / web Tasks / caldo
```

- The widget is patched for **one line**: `tuxedo` → `~/.local/bin/tuxedodav`
  (see `patch-widget.sh`; original saved as `Tasks.qml.orig`; needed again
  after a plugin update).
- The daemon mirrors the CalDAV list into `~/todo.txt` (+ `done.txt`), so the
  widget shows phone/web/caldo edits within 60s without any widget change.
- Line numbers map to tasks through a sidecar (`~/.local/state/tuxedodav/uids.json`),
  refreshed every pull under a flock shared with the daemon.

## Multi-list mode (v2)

Nextcloud keeps several lists (each shows as a Reminders list on phones), while
the tuxedo mirror merges **all configured lists into one todo.txt**. Each task
line carries its source list as a `+List` tag (spaces become `_`):

```
(A) 2026-10-26 buy oat milk +shopping due:2026-11-03
tidy desk @home +todo
```

- Routing: a `+List` token on an `add`/`replace` line chooses the collection —
  `replace 3 "buy oats +shopping"` moves line 3's task into the `shopping`
  list. Without a token, `replace` keeps the task's list and `add` uses
  `defaultList`.
- The routing token is consumed: it never appears in `SUMMARY` or `CATEGORIES`
  (Nextcloud only sees the task's location, not a duplicate label).
- `+`-tokens that don't match a configured list stay ordinary project tags.
- Only the first matching `+List` token routes; remaining ones stay projects.

## Mapping (todo.txt ↔ VTODO)

| todo.txt | CalDAV VTODO |
|---|---|
| plain words | `SUMMARY` (clean title — no +/@ tokens) |
| `+project`, `@context` | `CATEGORIES` labels, written **with** the prefix (`+home`, `@shop`) |
| bare label made in NC web | rendered as `@label` context (renamed on next write) |
| `due:YYYY-MM-DD` | `DUE;VALUE=DATE` |
| `(A)` / `(B)` / `(C)` | `PRIORITY` 1 / 5 / 9 |
| leading `YYYY-MM-DD` | `CREATED` (added if missing) |
| `x YYYY-MM-DD`, done.txt | `STATUS:COMPLETED` + `COMPLETED` |
| deletion | `DELETE` from the collection |

Notes for v1: one list per mirror file (config `list`); due dates are
date-only; priorities outside 1/5/9 (edited server-side) render without a
letter. Multi-list merge is a v2 idea.

## Setup on dell

```bash
tuxedodav init-config --user <nc-user> --password <app-password> \
    --server https://cloud.021407.xyz --list <list-name>
tuxedodav doctor          # end-to-end REPORT/PUT/GET/DELETE round trip
systemctl --user enable --now tuxedodav.service
```

Widget verbs used by Tasks.qml: `add <line>`, `do <n>…`, `replace <n> <line>`,
`del <n>… -f` (flags accepted, ignored). All mirror mutations go through the
same flock as the daemon, so daemon and verbs never interleave.

Backups/revert: `Tasks.qml.orig` + `systemctl --user disable --now tuxedodav`
restores stock behavior (plus removing the binary).