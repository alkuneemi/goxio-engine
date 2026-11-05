# GoXIO — Omxio deployment notes

GoXIO is the Omxio-branded public engine for live server topology maps.
It is based on [graph-go](https://github.com/guilherme-grimm/graph-go) (AGPL-3.0).

## Read-only Docker socket proxy (required in production)

Do **not** mount the host Docker socket directly into GoXIO on production hosts.
A `:ro` bind mount does **not** restrict Docker API calls; anything that can
reach the socket can still control the daemon.

Always put [`tecnativa/docker-socket-proxy`](https://github.com/Tecnativa/docker-socket-proxy)
(or equivalent) in front, allow only read endpoints (`CONTAINERS`, `EVENTS`,
`PING`, `VERSION`), set `POST=0`, and point GoXIO at it:

```yaml
environment:
  DOCKER_HOST: tcp://docker-proxy:2375
```

See `docker-compose.demo.yml` for a working example. Never open the proxy or
GoXIO UI ports to the public internet; place them behind Omxio auth.

## Vaulted credentials (preferred)

Prefer injecting database and service credentials from a vault / secret store
into environment variables (or a file mounted mode `600` from the vault),
instead of writing passwords into `config.yaml`.

Examples:

```bash
export GOXIO_POSTGRES_DSN='postgres://…'   # from vault
# config.yaml references env expansion where supported, or Omxio injects a
# generated config at runtime from vaulted values and never persists secrets
# into the public engine git tree.
```

Rules:

- Never commit real passwords, tokens, private keys, or DSNs.
- Never log secrets. Adapter registry redacts metadata and error strings before
  they reach the UI, scan output, or MCP responses — keep it that way.
- Platform vault paths (Omxio) stay outside this AGPL engine repository.

## Network exposure

- Bind GoXIO to loopback or an internal Docker network only.
- Terminate TLS and authentication at the Omxio reverse proxy.
- Customers and platform admins use GoXIO through Omxio; raw engine ports stay private.

## Source offer (AGPL)

The UI header includes a visible **Source** link to
https://github.com/alkuneemi/goxio-engine. Keep that link (or an equivalent
corresponding-source offer) whenever a modified GoXIO serves users over a network.
