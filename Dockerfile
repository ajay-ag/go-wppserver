# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wppserver ./cmd

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 wpp

WORKDIR /app

COPY --from=build /out/wppserver /app/wppserver
COPY docs /app/docs

RUN mkdir -p /app/dbdata && chown -R wpp:wpp /app

USER wpp

VOLUME ["/app/dbdata"]
EXPOSE 8786

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${SERVER_PORT:-8786}/healthz" >/dev/null || exit 1

ENTRYPOINT ["/app/wppserver"]
CMD ["-mode=run"]
