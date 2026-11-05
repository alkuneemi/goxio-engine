<div align="center">

# GoXIO

**Omxio server map engine** — based on [graph-go](https://github.com/guilherme-grimm/graph-go)

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Source](https://img.shields.io/badge/Source-alkuneemi%2Fgoxio--engine-green.svg)](https://github.com/alkuneemi/goxio-engine)

**EN:** Put the server IP in, and GoXIO draws everything on it — what is running, what is idle, and what is eating the disk — without writing to the host.

**AR:** حط IP السيرفر، وGoXIO يرسم لك كل شي فيه: إيش شغال، إيش نايم، وإيش ماكل المساحة، بدون ما يلمس شي.

</div>

---

## What this is

**GoXIO** is the public, AGPL-licensed topology engine used inside the Omxio
platform. Customers and platform admins open it through Omxio auth; this repo
is the corresponding source for the engine.

It auto-discovers Docker containers, Kubernetes workloads, PostgreSQL / MySQL /
MongoDB / Redis / Elasticsearch / S3 / HTTP services, and serves a live
interactive graph with health over WebSocket.

## Upstream pin

| Field | Value |
|---|---|
| Upstream project | https://github.com/guilherme-grimm/graph-go |
| Pinned commit SHA | `6c4f822b7f256f8e76fc082bf39861bdbcb727f8` |
| Upstream tip at fork | `6c4f822` — *Merge pull request #59 from guilherme-grimm/fix/compose-adapter-collision* |
| License | AGPL-3.0 (see `LICENSE`) |
| Copyright / attribution | see `NOTICE` |

Go module path remains `github.com/guilherme-grimm/graph-go` for compatibility
with upstream until a coordinated rename. The CLI binary and UI brand are **goxio** / **GoXIO**.

## Quick start (demo)

```bash
git clone https://github.com/alkuneemi/goxio-engine.git
cd goxio-engine
go run ./cmd/app demo
```

Open **http://localhost:8080**. The header shows **GoXIO** and a **Source** link
back to this repository (AGPL corresponding-source offer).

The space map (disk → volume → container → database → repo → GitHub account)
is included. Use the **Cards** and **Map** switch, then open a node for size,
path, repo, account, pressure, and confirm-gated delete / archive / transfer.
Those actions are recorded only — the process does not write to the host.

```bash
# scripted hierarchy, no Docker required
make run-space-demo

# UI dev server (proxies /api to :8080) in another terminal
make run-frontend
```

Run notes: [`docs/GOXIO-RUN.md`](docs/GOXIO-RUN.md).

## Production on Omxio hosts

1. **Read-only Docker socket proxy** — required. Never mount the raw Docker
   socket into GoXIO on production. Details: [`docs/GOXIO-OMXIO.md`](docs/GOXIO-OMXIO.md)
   and `docker-compose.demo.yml` (`tecnativa/docker-socket-proxy`, `POST=0`).
2. **Vaulted credentials** — prefer secrets from the Omxio vault injected as
   env vars; never commit real DSNs/passwords; never log secrets (redaction is
   enabled in the adapter registry).
3. **No public ports** — place GoXIO behind Omxio login; do not expose `:8080`
   to the internet.
4. Keep the visible **Source** link (or equivalent source offer) in any modified build.

Example (proxy pattern):

```bash
# DOCKER_HOST points at the read-only proxy, not unix:///var/run/docker.sock
docker run --rm -e DOCKER_HOST=tcp://docker-proxy:2375 goxio:local serve
```

## Commands

| Command | Purpose |
|---|---|
| `goxio demo` | Seeded Compose demo |
| `goxio serve` | HTTP + discovery + live UI (default) |
| `goxio scan` / `export` | One-shot graph (`json`, `mermaid`, `markdown`, `dot`) |
| `goxio validate` | Config check |
| `goxio mcp` | MCP server for agents |
| `goxio version` | Version / commit / build date |

Build:

```bash
make build          # → bin/goxio (frontend embedded)
./bin/goxio serve
```

## Capabilities (inherited from graph-go)

| Capability | Details |
|---|---|
| Docker | Containers, labels (`graphgo.*`), events via read-only API |
| Kubernetes | Namespaces, Deployments, Pods, Services, informers |
| PostgreSQL / MySQL | Tables + FK edges |
| MongoDB / Redis / ES / S3 | Collections, keyspaces, indices, buckets |
| HTTP services | Health + dependency edges |
| Live health | WebSocket updates |

## Security

- Authorized infrastructure only (systems you own or have permission to map).
- Prefer vaulted env for DB and cloud credentials.
- Never log secrets; metadata and errors are redacted before UI / MCP / scan output.
- Production Docker access must go through a read-only socket proxy.

## License and source

This engine is **AGPL-3.0**. If you modify GoXIO and let others use it over a
network, you must offer them the corresponding source of your modified version.
The Omxio platform application that wraps GoXIO remains separate when the engine
runs as an isolated service exchanging JSON only.

- License text: [`LICENSE`](LICENSE)
- Attribution: [`NOTICE`](NOTICE)
- Public source: https://github.com/alkuneemi/goxio-engine
- Upstream: https://github.com/guilherme-grimm/graph-go @ `6c4f822b7f256f8e76fc082bf39861bdbcb727f8`

## العربية (ملخص)

**GoXIO** محرك خريطة السيرفر في منصة أومكسيو. حط IP السيرفر، وGoXIO يرسم لك كل شي فيه: إيش شغال، إيش نايم، وإيش ماكل المساحة، بدون ما يلمس شي (قراءة فقط عبر وسيط Docker).

- الرخصة AGPL-3.0: لازم يبقى مصدر المحرك المعدّل متاحاً علناً (هذا المستودع).
- كلمات المرور من الخزنة، ما تتنكتب في الشات ولا في اللوجات.
- ما نفتح منافذ للعموم؛ الاستخدام من داخل أومكسيو للعملاء وإدارة المنصة الأم.
