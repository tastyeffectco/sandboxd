# Upgrading sandboxd

sandboxd upgrades **in place** with a single command. It backs up your database
and `.env` first, applies new migrations, health-checks the new stack, and
**rolls back automatically** if the new version doesn't come up — so it's safe to
run on a production instance.

> Migrations are **additive only** (new tables/columns, never destructive), so an
> upgrade never rewrites or drops your existing data.

## Check your version

```bash
./upgrade.sh --check      # shows your current checkout vs the latest release; changes nothing
```

The running control plane also reports its version and whether an update is
available (anonymous release check) at `GET /v1/settings` → `version`,
`update_available`, `latest_version`, `changelog_url`.

## Upgrade

From your sandboxd source directory (the folder with `install.sh`):

```bash
./upgrade.sh
```

What it does, in order:

1. **Backs up** the database (`state/sandboxd.db`) and `.env` to
   `<data-dir>/backups/<timestamp>/`, and records the current commit.
2. **Fetches** the new code.
3. **Rebuilds + restarts** the stack (`docker compose build && up -d`). New
   migrations apply automatically when the control plane boots.
4. **Health-checks** the new control plane. If it doesn't become healthy, it
   **restores the backup, reverts the code, and brings the previous version back
   up**, then exits non-zero.

If it succeeds, your backup is kept at the path printed at the end — delete it
once you're satisfied.

## Roll back manually

Automatic rollback covers a failed upgrade. To revert a *successful* upgrade you
weren't happy with, restore from a backup:

```bash
cd <your-sandboxd-source-dir>
BK=<data-dir>/backups/<timestamp>          # pick the one you want
git reset --hard "$(cat "$BK/previous-commit.txt")"
cp "$BK/sandboxd.db" <data-dir>/state/sandboxd.db
docker compose --profile console up -d --build
```

## Pin a specific version

By default `./upgrade.sh` tracks the `main` branch (where releases currently
land). To pin a tagged release or branch:

```bash
./upgrade.sh v0.4.0
```

## Notes

- **Backups accumulate** under `<data-dir>/backups/`. They're small (a SQLite
  file + `.env`); prune old ones when you like.
- **Your apps and data are untouched** by an upgrade — only the sandboxd images
  and schema move forward.
- **Update detection** relies on the anonymous release check (part of the
  telemetry heartbeat). It sends no code or personal data and is opt-out with
  `SANDBOXD_TELEMETRY=off` — the upgrade itself works regardless.

## Troubleshooting

- **"could not reach GitHub"** on `--check` — network/rate-limit; the upgrade
  still works, it just can't show the latest tag.
- **Upgrade rolled back** — the new control plane failed its health check. Look
  at `docker compose logs sandboxd`; your previous version is already running and
  your data was restored from the backup.
- **Ran out of disk mid-build** — free space (old images: `docker image prune`)
  and re-run; your backup from the aborted attempt is still under
  `<data-dir>/backups/`.
