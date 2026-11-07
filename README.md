# wppserver (hardened fork)

A small, self-hosted WhatsApp sender for the RMS receipt flow.

This fork exists because the upstream `v1.0.0` release is pinned to a March 2023
`whatsmeow` build, which WhatsApp rejects with `failure reason="405"`. The fork
updates the protocol library to the current release and hardens the parts that
must not run in production.

> **Unofficial client.** This is not the Meta WhatsApp Cloud API. It uses a
> linked WhatsApp device and violates WhatsApp's Terms of Service. A number can
> be banned. Use a dedicated number and always keep a fallback (share link, SMS,
> email, print).

## What changed from upstream

- `go.mau.fi/whatsmeow` updated from `2023-03` to the current release.
- JWT secret is read from `AUTH_JWT_SECRET`; the hardcoded
  `your-256-bit-secret` is gone. Tokens expire after
  `AUTH_TOKEN_TTL_MINUTES` (default 60).
- JWT verification now rejects non-HMAC signing methods.
- Binds to `127.0.0.1` by default. Do not expose this API publicly.
- Public self-registration is disabled. Admins can still create users.
  Set `ALLOW_USER_REGISTRATION=true` only if you really need it.
- HTTP handlers no longer call `log.Panic` / `log.Fatal`. A bad recipient or a
  failed send returns a JSON error instead of killing the process.
- Nil-safe device/session handling.
- WhatsApp protocol logging defaults to `INFO`; `DEBUG` is opt-in.
- Contact scraping and contact-list endpoints are no longer routed.
- `/healthz` endpoint for Docker/systemd health checks.
- Explicit data directory via `-data-dir` / `WPP_DATA_DIR`, so the binary can
  live in a read-only location while `.env`, `dbdata/` and `debug.log` stay in
  a writable state directory.
- Dockerfile updated, non-root runtime, persistent `dbdata` volume.
- `go.mod` requires Go 1.26+.

## Requirements

- Go 1.26+ (build only — the release binary needs nothing installed)
- A spare phone number with WhatsApp
- A persistent disk for the data directory (WhatsApp session credentials live in
  `dbdata/`)

## Releases

Tagged builds are published as static binaries (no libc, no Go toolchain
required):

```bash
curl -fsSLO https://github.com/ajay-ag/go-wppserver/releases/latest/download/wppserver-linux-amd64
curl -fsSLO https://github.com/ajay-ag/go-wppserver/releases/latest/download/SHA256SUMS

shasum -a 256 -c SHA256SUMS --ignore-missing
chmod +x wppserver-linux-amd64
```

Available targets: `linux-amd64`, `linux-arm64`, `darwin-amd64`,
`darwin-arm64`. On macOS, remove the quarantine flag after downloading:

```bash
xattr -d com.apple.quarantine wppserver-darwin-arm64
```

## Layout

The binary and its state are separate:

```
/opt/rms-wppserver/wppserver              # read-only release artifact
/etc/rms-wppserver/env                    # secrets (root:rms-wpp 0640)
/var/lib/rms-wppserver/.env               # optional, if not using EnvironmentFile
/var/lib/rms-wppserver/dbdata/            # WhatsApp session (0700, back this up)
/var/lib/rms-wppserver/debug.log          # 0600
```

`-data-dir` (or `WPP_DATA_DIR`) selects the state directory. It defaults to the
binary directory, which is convenient for a self-contained folder but wrong for
`vendor/bin` or `/opt`. Relative paths inside the data dir (`dbdata/`, `docs/`
excluded) resolve against it.

## Quick start

```bash
mkdir -p /var/lib/rms-wppserver
cp .env.example /var/lib/rms-wppserver/.env
```

Edit `.env`:

```dotenv
SERVER_ADDRESS=127.0.0.1
SERVER_PORT=8786
AUTH_INITIAL_EMAIL=admin@example.com
AUTH_INITIAL_PASSWORD=a-long-password
AUTH_JWT_SECRET=<openssl rand -base64 48>
```

Run:

```bash
# From the repo (data dir must be explicit, otherwise state lands in the
# temporary build dir that `go run` creates)
go run ./cmd -mode=run -data-dir="$PWD"

# Or build a release binary and run it from anywhere
make build
./bin/wppserver -mode=run -data-dir=/var/lib/rms-wppserver
```

Startup logs print the resolved data dir and env file:

```
main.go:116: Data dir: /var/lib/rms-wppserver
main.go:117: Env file: /var/lib/rms-wppserver/.env
```

Pair the number once:

```bash
# 1. Get a token
curl -s -X POST http://127.0.0.1:8786/v1/auth \
  -d 'username=admin@example.com&password=a-long-password'

# 2. Get the QR (base64 PNG)
curl -s http://127.0.0.1:8786/v1/device/login \
  -H "Authorization: Bearer <token>"
```

Decode `base64qrcode` into an image and scan it with WhatsApp →
Linked devices. Then check:

```bash
curl -s http://127.0.0.1:8786/v1/session/status \
  -H "Authorization: Bearer <token>"
```

Send a text:

```bash
curl -s -X POST http://127.0.0.1:8786/v1/chat/send/text \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"phone":"919876543210","body":"Hello from RMS"}'
```

`phone` must be in international format without `+` or spaces. The sender also
accepts `+` and an explicit `@s.whatsapp.net` suffix.

## API

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/auth` | Get a JWT (`username`, `password`) |
| `GET` | `/v1/auth/refresh` | Refresh a JWT |
| `GET` | `/v1/device/login` | Get a base64 QR code |
| `GET` | `/v1/device/logout` | Unlink the device |
| `GET` | `/v1/session/status` | Connection/login state |
| `POST` | `/v1/session/connect` | Reconnect |
| `POST` | `/v1/session/disconnect` | Disconnect without unlinking |
| `POST` | `/v1/chat/send/text` | Send text |
| `POST` | `/v1/chat/send/image` | Send image (base64 data URL) |
| `POST` | `/v1/chat/send/document` | Send PDF/document (base64 data URL) |
| `GET` | `/healthz` | Unauthenticated liveness probe |

Swagger UI: `http://127.0.0.1:8786/docs/api/`

## Docker

```bash
cp .env.example .env   # edit it first
docker build -t rms-wppserver .
docker run -d \
  --name rms-wppserver \
  --restart unless-stopped \
  -p 127.0.0.1:8786:8786 \
  --env-file .env \
  -v rms-wppserver-data:/app/dbdata \
  rms-wppserver
```

The `-p 127.0.0.1:...` form keeps the port off the public interface.

If Laravel runs in another container, put both on a private Docker network and
set `SERVER_ADDRESS=0.0.0.0`; do **not** publish the port to the internet.

## systemd

```ini
[Unit]
Description=RMS WhatsApp sender
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=rms-wpp
Group=rms-wpp
WorkingDirectory=/var/lib/rms-wppserver
ExecStart=/opt/rms-wppserver/wppserver -mode=run -data-dir=/var/lib/rms-wppserver
EnvironmentFile=/etc/rms-wppserver/env
Restart=always
RestartSec=5

NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
ReadWritePaths=/var/lib/rms-wppserver
UMask=0077

[Install]
WantedBy=multi-user.target
```

`EnvironmentFile` is read before privileges are dropped, so
`/etc/rms-wppserver/env` can be `root:root 0600`. Run exactly one instance per
data directory: SQLite will report `database is locked` otherwise.

## Laravel integration

The sender is deliberately dumb. Laravel owns the message text, queue, retries
and receipt URL:

```php
Http::withToken($token)
    ->post(config('services.whatsapp_sender.url').'/v1/chat/send/text', [
        'phone' => $phone,
        'body'  => $message,
    ])
    ->throw();
```

Recommended: one global number for the whole RMS, one pairing, and a central
outbox table keyed by `(tenant_id, order_id)` for idempotency. If the order has
no phone number, do not prompt the cashier; fall back to the signed receipt
link.

## Operational notes

- Keep `dbdata/` on a persistent volume. Losing it means re-pairing.
- One global number is a single point of failure. If it is banned, every tenant
  loses silent sending. Keep the share link as fallback.
- Re-pair after `session/status` reports not logged in.
- Keep volume low; this is for transactional receipts, not bulk marketing.
- Logs do not contain customer message bodies by default. With
  `WHATSAPP_LOG_LEVEL=DEBUG` the protocol library logs more metadata; do not use
  it in production.

## Privacy

This fork contains no analytics, telemetry or third-party reporting. It talks to
WhatsApp servers (required), stores its session and users in the local
SQLite/PostgreSQL database, and exposes only the API above. The upstream
internet-connectivity check to `clients3.google.com` has been removed.

## License

Upstream ships an **empty `LICENSE` file** (GitHub reports `NOASSERTION`). This
fork keeps that status; it is intended for internal use. Do not redistribute it
without clarifying the license with the original author.
