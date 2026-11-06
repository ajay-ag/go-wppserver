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
- Dockerfile updated, non-root runtime, persistent `dbdata` volume.
- `go.mod` requires Go 1.26+.

## Requirements

- Go 1.26+ (or Docker)
- A spare phone number with WhatsApp
- A persistent disk for `dbdata/` (WhatsApp session credentials live there)

## Quick start

```bash
cp .env.example .env
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
go run ./cmd -mode=run
# or
make build
./bin/wppserver -mode=run
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
