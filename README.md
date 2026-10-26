# caldav-todo.txt

**Keep your `todo.txt` files and widget in sync with a CalDAV (VTODO) server.**

`tuxedodav` (binary; source of this repo) is a small Go program that turns any
tool that reads and edits a plain `todo.txt` file — a desktop widget, panel,
editor macro, or script — into a full client for a CalDAV task server such as
Nextcloud Tasks. The file stays front-and-centre as a *mirror*: the app keeps
working unchanged, while the real tasks live on the server and reach your
phone's Reminders app, web clients, and anything else that speaks CalDAV.

```
widget / editor (reads todo.txt, watches for changes)
        │  read  ▲
        ▼        │ re-written only when content changed
   ~/todo.txt + done.txt   ◀──── daemon pulls every N seconds
        ▲  add / do n / replace n / del n
        └── tuxedodav ── CalDAV ──▶ Nextcloud (VTODO) ⇄ iPhone Reminders, web, any DAV client
```

## Features

- **File watch + two-way sync**: the daemon (`tuxedodav sync`) pulls your task
  lists every `interval` seconds and only rewrites the mirror when content
  actually changed, so file-watchers don't churn. Edits made on the phone or
  web appear in the file within one tick; file-side edits made through the CLI
  reach the server instantly.
- **Multi-list merge**: several CalDAV lists are merged into one mirror file;
  each task carries its source list as a `+List` tag (spaces become `_`).
  Editing a task's `+List` tag **moves it between collections**.
- **Tag → list automation** (`autoList`): give a task any tag and sync creates
  the matching CalDAV list if it doesn't exist and files the task there —
  phone-side and file-side. Case-safe (a `dell` tag won't spawn a twin of your
  `Dell` list), with a runaway cap of 10 new lists per pass.
- **Collection discovery**: at startup the client PROPFINDs the account's
  calendar home and participates in every VTODO-capable list — lists never
  fall out of view because a config file forgot them. Per-list exclusions are
  supported (`excludes`) so app-generated boards (e.g. `app-generated--deck--…`)
  can stay out of the mirror.
- **Safe iCalendar handling**: proper TEXT escaping, line folding, RFC-compliant
  `CREATED`/`COMPLETED` (DATE-TIME), `DUE;VALUE=DATE`, `CATEGORIES` with commas
  — all covered by round-trip unit tests.
- **No lost edits**: every verb and the daemon share a `flock`; mirror writes
  are atomic (write+rename); line numbers map to objects through a refreshed
  UID sidecar.
- **`tuxedodav doctor`**: end-to-end REPORT/PUT/GET/DELETE round trip against
  the real server for quick validation.

## Mapping (todo.txt ↔ VTODO)

| todo.txt | CalDAV VTODO |
|---|---|
| plain words | `SUMMARY` (clean title — list routing tokens never leak) |
| `+project`, `@context` | `CATEGORIES` labels, written with the prefix (`+home`, `@shop`) |
| `+List` matching a configured/known list | **routing**: which collection the task lives in |
| any other tag (via `autoList`) | a CalDAV list named after the tag |
| `due:YYYY-MM-DD` | `DUE;VALUE=DATE` |
| `(A)` / `(B)` / `(C)` | `PRIORITY` 1 / 5 / 9 |
| leading `YYYY-MM-DD` | `CREATED` (added if missing) |
| `x YYYY-MM-DD`, done.txt | `STATUS:COMPLETED` + `COMPLETED` |
| deletion (`del <n>`) | `DELETE` from the collection |

## Commands

```
tuxedodav sync [--interval N] [--once]
tuxedodav add <todo.txt line>
tuxedodav do <lineNumber> [more...]
tuxedodav replace <lineNumber> <new line>
tuxedodav del <lineNumber> [more...] [-f accepted, ignored]
tuxedodav doctor
tuxedodav init-config --user <name> --password <app-password> \
    --server https://nextcloud.example.org --lists "Todo,Work,Personal" \
    [--default-list Todo] [--todo-file path] [--exclude Deck:_Personal] \
    [--no-auto-list]
```

Line-number verbs operate on 1-based line numbers of the current mirror file
(what the widget displays). `TODO_FILE`/`DONE_FILE` environment variables
override the configured mirror paths — this is how the bundled
[omarchy](https://omachy.com) bar-widget integration invokes the binary
(`omarchy-tuxedo-todo` gets one string patched: its `tuxedo` CLI becomes this
binary; see `patch-widget.sh`).

## Configuration

`~/.config/tuxedodav/config.json` (0600), written by `init-config`:

```json
{
  "username": "you",
  "password": "<app-password>",
  "server": "https://nextcloud.example.org",
  "lists": ["Todo", "Work", "Personal"],
  "defaultList": "Todo",
  "autoList": true,
  "excludes": ["Deck:_Personal"],
  "todoFile": "~/todo.txt",
  "stateDir": "~/.local/state/tuxedodav",
  "interval": 60
}
```

- `lists` is only a **seed**: discovery picks up the rest of the account.
- `defaultList` receives tasks created without a `+List`/tag.
- Auto-created lists are persisted back into `lists` automatically.

## Deployment

```bash
go build -o ~/.local/bin/tuxedodav .
tuxedodav init-config …
tuxedodav doctor    # REPORT/PUT/GET/DELETE round trip
```

Run continuously as a systemd user unit:

```ini
[Unit]
Description=tuxedodav - todo.txt ⇄ CalDAV (VTODO) sync
After=network-online.target

[Service]
ExecStart=%h/.local/bin/tuxedodav sync --interval 60
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
```

`systemctl --user enable --now tuxedodav` (add `loginctl enable-linger` to
keep it alive while logged out).

## Notes & scope

- Tested against Nextcloud (Sabre/Dav); uses only stdlib HTTP/XML.
- Due dates are date-only; `RRULE` recurrence is not parsed; priorities
  outside 1/5/9 (set server-side) render without a letter.
- Apple Reminders ignores `CATEGORIES` (no label sync on iOS), ignores
  client→server recurrence, and pushes its own edits on Apple's schedule —
  that's client behaviour, not this daemon's.
- The widget side expects a mirror-style consumer that (a) displays the file
  and (b) performs edits by appending a line or addressing line numbers —
  the todo.txt idioms.

## License

MIT