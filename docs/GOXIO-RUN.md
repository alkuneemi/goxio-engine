# GoXIO run notes

The space map is disk → volume/folder → container → database → repo → GitHub account.
Cards drill that chain. Map is the existing force-directed graph (switch layouts in the header).
Click a node for path, size, containers, repo, GitHub account, activity, cleanable, and pressure when the collector has it.

Actions (delete, archive, transfer) open a confirm dialog and require the exact word `DELETE`, `ARCHIVE`, or `TRANSFER`.
They update the in-memory map and the audit log only. The process does not run `rm`, `docker rm`, or any other host write.

## Demo snapshot (no Docker)

```bash
make run-space-demo
```

Open http://localhost:8080. For the Vite UI while iterating:

```bash
make run-frontend   # http://localhost:5173 proxies /api to :8080
```

`GOXIO_SPACE_SOURCE=demo` serves the scripted hierarchy (two disks, an orphan volume, a core Postgres database, repos, two GitHub accounts).

## Host collector (read-only)

```bash
GOXIO_SPACE_SOURCE=host go run ./cmd/app serve
# or
GOXIO_SPACE_SOURCE=auto go run ./cmd/app serve
```

`auto` (the default) uses the host when `df` or Docker returns a disk, volume, or container. Otherwise it loads the demo snapshot.

What it runs:

- `df -B1 -PT` (virtual filesystems skipped)
- Docker API list/inspect of containers and volumes via `DOCKER_HOST` (GET only)
- `du -sx -B1` on specific volume mountpoints, capped and timed out

It does not mount the raw Docker socket in the demo compose. `docker-compose.demo.yml` puts `tecnativa/docker-socket-proxy` in front with `POST=0` and `VOLUMES=1`.

Database names come from `POSTGRES_DB`, `MYSQL_DATABASE`, or `MONGO_INITDB_DATABASE`. Passwords and DSNs are not copied. GitHub links come from `goxio.repo`, `goxio.github`, or `org.opencontainers.image.source`.

A container's own filesystem is what `df` sees. On a VM that is the VM. Inside the engine container it is the container, not the customer's host. The Omxio collector should `POST /api/space/ingest` with the host snapshot instead of relying on `df` inside the engine.

## Ingest

```bash
curl -s -X POST localhost:8080/api/space/ingest \
  -H 'content-type: application/json' \
  -H "X-Goxio-Token: $GOXIO_INGEST_TOKEN" \
  -d @snapshot.json
```

Set `GOXIO_INGEST_TOKEN` in any shared environment. Empty token allows ingest (local dev). Metadata keys that look like passwords are masked before the UI sees them. `host_writes` is forced false.

In `auto` mode an ingested snapshot stays until restart. `host` mode refreshes from the host about every 45s.

## Actions

```bash
curl -s -X POST localhost:8080/api/space/actions \
  -H 'content-type: application/json' \
  -d '{"node_id":"space-volume-old-cache","action":"delete","confirm":"DELETE"}'
```

Missing or wrong `confirm` returns 400 and changes nothing. Core nodes, disks, and GitHub accounts return 409 for delete and archive. Transfer needs `target_account` and/or `target_server`.

`GET /api/space/actions` is the audit list. `applied_to_host` is always false.

## Build

```bash
make build          # web UI + bin/goxio
GOXIO_SPACE_SOURCE=demo ./bin/goxio serve
```

```bash
make test-backend
make test-frontend  # tsc --noEmit
```

Docker:

```bash
GOXIO_SPACE_SOURCE=demo docker compose -f docker-compose.demo.yml up --build
```

Do not publish port 8080. Put the engine behind Omxio auth. Keep the header **Source** link (AGPL).
