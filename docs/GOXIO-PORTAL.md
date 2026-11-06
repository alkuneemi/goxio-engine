# GoXIO portal

GoXIO sign-in is its own account system. It is not an Omxio customer login. A session cookie is the only browser credential. Each account sees only the servers provisioned for it.

The engine does **not** SSH to a server address, and it does not read the host it is running on into a tenant map. Inventory arrives as a snapshot posted with that server’s ingest token, or loaded by an operator from a file. When a server has no snapshot, the API returns an empty graph with `status: pending`. It does not substitute the demo map or this machine’s disks.

## Provision a tenant

Run these on the engine host. Do not commit the password, the ingest token, or `data/portal/`.

```bash
export GOXIO_DATA_DIR=/var/lib/goxio/portal
install -d -m 700 "$GOXIO_DATA_DIR"

# Password is read from the environment or a terminal prompt. There is no --password flag.
export GOXIO_PORTAL_PASSWORD='replace-with-a-long-password'
goxio portal account create --login sahabify
unset GOXIO_PORTAL_PASSWORD

# Address is a label. GoXIO does not dial it and does not use an SSH key.
# The ingest token is printed once. Redirect it to a mode 0600 file on the collector host.
goxio portal server add --login sahabify --name sahabify --address 187.127.114.87 \
  > /root/sahabify.ingest.token
chmod 600 /root/sahabify.ingest.token

# Optional. The same slug also accepts sahabify@gmail.com on sign-in (any domain,
# local part exactly sahabify). A successful sign-in stores that address as an alias.
# This command does the same thing without waiting for a sign-in.
goxio portal account alias --login sahabify --alias sahabify@gmail.com
```

`Sahabify` and `Sahabify@Gmail.com` are the same account as `sahabify`. `saha.bify@gmail.com` and `sahabify+ops@gmail.com` are not. A wrong password does not open a session and does not save an alias.

The password is compared byte for byte with the stored secret. A trailing space, a newline, or a different capital letter does not match. The sign-in form can show the password and warns when the field has leading or trailing space or Caps Lock; it does not remove them. The login name is still case-folded, and spaces around the login name are not part of the name.

On process start, mixed-case login and alias strings in account files are rewritten to that canonical form. The password hash, account id, and session files are not rewritten. Two records that would become the same name are not merged.

A server added while `goxio serve` is already running is accepted on the next ingest. The process re-reads token files when the token is unknown. A restart is not required.

## Create an account

The sign-in screen includes **إنشاء حساب**. It posts `login`, `password`, and `confirm` to `POST /api/portal/register`. The server generates the account id. A body that also names an account, tenant, or server is rejected and creates nothing. The new account has no servers and no snapshot. It cannot read another account’s map, and the form does not ask for an SSH key or a host.

If that login is already a slug, an alias, or the exact local part of an existing slug, the response is `409` and nothing is attached. There is no reset email. A failed sign-in stays `401` with the same “invalid login or password” text.

The browser sends `X-GoXIO-Request: 1`. When it also sends `Origin` or `Referer`, that host must match the request host. `X-Forwarded-Host` is used only when the TCP peer is on `GOXIO_TRUSTED_PROXIES`. The session cookie stays HttpOnly, Secure unless `GOXIO_COOKIE_SECURE=0`, and SameSite=Strict.

## Omxio sign-in

The button **الدخول بحساب أومكسيو** starts an OpenID Connect authorization-code flow with PKCE. GoXIO does not collect or store the Omxio password, and it does not read Omxio’s password database.

On 2026-11-06 the repository `https://github.com/alkuneemi/omxio` was not readable with this environment’s GitHub credentials (the API returned 404). No issuer URL was guessed from the product name. Nothing in this build calls an Omxio host unless an operator sets all of these:

| Variable | Meaning |
|---|---|
| `GOXIO_OIDC_ISSUER` | Exact issuer URL. Discovery is `{issuer}/.well-known/openid-configuration`. |
| `GOXIO_OIDC_CLIENT_ID` | Client id registered at that issuer. |
| `GOXIO_OIDC_CLIENT_SECRET` or `GOXIO_OIDC_CLIENT_SECRET_FILE` | Client secret. The file may end with one newline. It is not logged. |
| `GOXIO_OIDC_REDIRECT_URI` | Exact redirect URI, for example `https://goxio.example/api/portal/oidc/callback`. |

If any of those is missing, the button does not navigate. It says Omxio sign-in is not configured. A provider outage redirects to `/?sso_error=unavailable` and local GoXIO login still works. A rejected token redirects to `/?sso_error=rejected` and does not open a session.

The provider contract, which is not implemented in the Omxio repo from here:

- Authorization endpoint accepts `response_type=code`, `scope=openid`, `state`, `nonce`, `code_challenge`, `code_challenge_method=S256`, and the exact `redirect_uri`.
- Token endpoint accepts `authorization_code`, the same `redirect_uri`, `code_verifier`, and HTTP Basic client authentication.
- ID token is RS256. Required claims: `iss` equal to the configured issuer, `aud` containing the client id, `exp`, `iat`, `nonce`, and a stable `sub`.
- `authorization_endpoint`, `token_endpoint`, and `jwks_uri` must be on the issuer’s host. GoXIO will not follow them anywhere else.
- An `email` claim is ignored for account matching. A new `iss`+`sub` gets an empty GoXIO tenant. It is not attached to an existing tenant because the email matches. Linking happens only when that GoXIO user is already signed in and posts to `/api/portal/oidc/link` with the session CSRF token.

The tests under `internal/server/oidc_test.go` speak this protocol to an in-process provider. They are not a live sign-in against Omxio.

`187.127.114.87` is the remote server for the sahabify account. It is not the Omxio KSA host. Do not point this server record at the machine that serves `goxio.sa.omxio.com`.

On the remote host (or a container whose mount namespace is that host), collect read-only and post the snapshot. `GOXIO_HOST_ROOT` rewrites a bind such as `/host-disk` back to host paths. Leave it unset when `df` already sees the host mounts.

```bash
export GOXIO_HOST_ROOT=/host-disk   # only if df shows that collector mount
export GOXIO_DU_BUDGET=40           # probes per pass; unfinished paths stay pending or timeout
# Each pass is bounded. A directory that does not finish is split into
# children and continued on the next pass. Repeat until sizes_partial is 0
# or the remaining paths are unreadable. Do not expect one 8s du of /usr or /var to finish.
f=/var/lib/goxio/sahabify-snapshot.json
GOXIO_DU_BUDGET=40 goxio collect -o "$f"
GOXIO_DU_BUDGET=40 goxio collect --resume "$f" -o "$f"
# safe to run again; measured subtrees are not walked from scratch

curl -fsS -X POST https://goxio.example/api/space/ingest \
  -H "Authorization: Bearer $(cat /root/sahabify.ingest.token)" \
  -H 'content-type: application/json' \
  --data-binary @/tmp/snapshot.json
```

The token is stored only as a hash next to a server-side pepper (`$GOXIO_DATA_DIR/pepper`, mode 0600). It is not returned by the servers API and not written to the info log.

An operator who already has a snapshot file on the engine host can load it without HTTP:

```bash
goxio portal snapshot load --login sahabify --server sahabify --file /secure/snapshot.json
```

That file must be sahabify’s inventory. Loading a collect taken on the KSA host would show the wrong machine.

## Session cookies

| Setting | Default |
|---|---|
| Name | `__Host-goxio_session` when `GOXIO_COOKIE_SECURE` is not `0` |
| HttpOnly | yes |
| SameSite | Strict |
| Secure | yes, unless `GOXIO_COOKIE_SECURE=0` (local HTTP tests only) |
| Lifetime | 12 hours, revoked on sign-out |

Mutating browser calls send `X-GoXIO-Request: 1` and `X-CSRF-Token` from `GET /api/portal/me`. Login and signup are rate-limited per client address and login (8 failures / 15 minutes in one process). A `429` response includes `Retry-After`. The browser shows that wait separately from a wrong password and from a network failure.

`X-Forwarded-For` and `CF-Connecting-IP` are ignored unless `GOXIO_TRUST_PROXY=1` and the TCP peer is inside `GOXIO_TRUSTED_PROXIES` (comma-separated IPs or CIDRs). A client that is not on that list cannot open a new bucket by sending a documentation address. Behind a trusted proxy, the address used is `CF-Connecting-IP` when that proxy set it, otherwise the rightmost forwarded hop that is not itself a trusted proxy. Two people behind the same proxy therefore do not share one bucket, and an address to the left of the proxy’s own hop does not. `GOXIO_TRUST_PROXY=1` with an empty list does not honor forwarded headers. The CORS `allowed_origins` list is unchanged.

`/health` stays unauthenticated and returns `{"status":"ok"}` for the process probe. It has no tenant data. `/api/health` requires a session and a server id.

`curl` against `/websocket` with HTTP/2 and no WebSocket upgrade can return **426**. That is the handshake refusing a normal HTTP request. It is not a failed login. Browsers and a WebSocket client send the upgrade on HTTP/1.1; the session cookie is checked, and a revoked session closes the socket on the next tick (about 5 seconds).

`goxio scan` / `goxio export` and `goxio mcp` are operator CLIs. They are not portal routes and they are not given a tenant session. The map API does not merge their local discovery into a customer graph.

## What the map will and will not claim

- Every listed container is kept. Inspect that stops early is `inspect_state: partial` and `truncated_count` on the summary. There is no silent cap at 60.
- `/api/space` counts volumes, images, folders, and mounts separately. An image is not a volume. Two tags of one digest are one image. A bind of a volume mountpoint is not a second volume.
- A directory `du` that does not finish is split into its children and continued on the next `goxio collect --resume`. Finished subtrees are kept. `size_state: partial` with `size_bytes_so_far` and `size_progress` means the walk is in progress, not that the total is known. `unreadable` has no size. `complete` stays false until every such walk is measured or explicitly unreadable. `listing_complete` can still be true. The HTTP ingest path does not run `du`.
- `df` rows are filesystem mounts. Partitions such as `sda1`, `sda15`, and `sda16` share a device-group node and are not added together into a fake disk total. A mapper device stays its own group.
- Images and generic path roots (`/var`, `/var/lib`, `/var/lib/containerd`, `/opt`, `/root`, `/usr`, and similar) are nodes when the collector can see them. A timed-out `du` still leaves the node.
- Database nodes are created only from database-name environment variables or `goxio.database`. Repo and GitHub nodes are created only from explicit GitHub labels. Other containers stay on the map with `provenance: unknown`.
- Child sizes can overlap the filesystem total from `df`. They are marked `overlaps_parent` and are not a second copy of the disk.
- Paths that look like SSH keys or credential files are stored as `(redacted)` and their contents are not read.

## Deploy, backup, rollback

This document does not deploy anything.

Before replacing a binary:

```bash
systemctl stop goxio   # or the unit/container name you actually use
tar -C /var/lib/goxio -czf "/var/backups/goxio-portal-$(date -u +%Y%m%dT%H%M%SZ).tar.gz" portal
```

Rollback is the previous binary plus that archive. Accounts created by self-service signup after the backup disappear with the archive, and sessions issued after it stop working. That is the restore. No email is sent and no host is dialed.

```bash
systemctl stop goxio
tar -C /var/lib/goxio -xzf /var/backups/goxio-portal-TIMESTAMP.tar.gz
# restore the previous goxio binary, then
systemctl start goxio
```

There is no separate migration command. The first start of this binary rewrites mixed-case login strings as described above. Take the backup before that start.

Actions in the UI record delete, archive, and transfer intent only. They do not SSH, delete remote files, or change Docker on the customer server.

Proxy basic auth in front of the site is not tenant isolation. Keep it only as a temporary extra gate. Viewers must use portal accounts.

## Gaps that are still real

This is not a claim that the portal is fully secure.

- One process and one data directory. Sessions and snapshots are files. A second replica needs that directory shared, and the login rate limit is still per process.
- No second factor and no password-reset email. Self-service signup creates an empty account. An operator still resets a password by replacing the hash out of band. GoXIO does not send mail.
- The engine will not pull a map over SSH. If the collector on the remote host does not post, the server stays pending.
- `goxio scan` and `goxio mcp` still describe the engine host’s own Docker or Kubernetes discovery when an operator runs them on that host. They are not served to portal users. Server stderr from discovery can still name local containers.
- A size walk that has not finished is `partial` or `timeout`, with progress saved in the snapshot. Repeating the same full-tree `du` is not how a large directory completes. Unreadable paths stay unreadable and are not stored as zero. `complete` stays false while any walk is unfinished.
- There is no MFA. A long password and the portal session are the demo bar.
- A long-lived WebSocket is checked again about every five seconds and then closed if the session was revoked. There is a short window.
- Cookie `Secure` is correct only when the browser talks HTTPS. Leave `GOXIO_COOKIE_SECURE` at its default in production.
- Path redaction is a name filter, not a guarantee that every secret path shape is recognized.
- `du` and Docker inspect are bounded per pass so the process stays up. A snapshot with timeouts is explicitly partial, not a finished disk total.
- No independent penetration test has been done on this build.
