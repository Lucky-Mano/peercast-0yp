# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`peercast-0yp` is a Go implementation of a PeerCast root server (YP / Yellow Page). It speaks the PCP binary protocol to accept channel registrations from PeerCast clients and serves channel listings over HTTP.

## Commands

```bash
go build ./...
go test ./...
go test -run TestFoo ./internal/pcp/...
go vet ./...
```

## Docker

compose ファイルは環境別に分かれている：
- `docker-compose.yml` — 共通（app のみ）
- `docker-compose.dev.yml` — 開発追加分（Caddy + PostgreSQL）
- `docker-compose.prod.yml` — 本番追加分（外部PostgreSQLへの接続設定）

```bash
# 開発
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
docker compose -f docker-compose.yml -f docker-compose.dev.yml restart app
docker compose -f docker-compose.yml -f docker-compose.dev.yml logs -f app
docker compose -f docker-compose.yml -f docker-compose.dev.yml down

# ホスト DB 接続の app 単体（Caddy なし。VPS 本番は yayaue.me/compose.yaml）
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
docker compose -f docker-compose.yml -f docker-compose.prod.yml restart app
docker compose -f docker-compose.yml -f docker-compose.prod.yml logs -f app
docker compose -f docker-compose.yml -f docker-compose.prod.yml down
```

## Code Structure

```
main.go                  — entry point, wires all components
internal/channel/        — Info, Track, Hit, HitList, Store (thread-safe registry)
internal/pcp/            — PCP root server: handshake, bcst parsing, session management
internal/archive/        — Recorder: polls Store every 1s, writes sessions/snapshots to PostgreSQL
internal/httpd/          — chi HTTP server: index.txt, /api/* endpoints
internal/repository/     — PostgreSQL access (SessionRepo, SnapshotRepo)
internal/config/         — TOML config loader
```

## Key Rules

- **Genre `yp` prefix**: channels whose `Genre` does not start with `yp` are not registered to this YP — exclude silently from `Store.AddHit` and `index.txt`. See `docs/protocol/genre.md`.
- **BCID immutability**: `Store.AddHit` rejects a mismatched `BroadcastID` once one is set (channel ownership check).
- **IP encoding**: IPv4 → 4 bytes reversed (little-endian); IPv6 → 16 bytes reversed. See `encodeIP`/`decodeIP` in `internal/pcp/` and `decodeIP` in `internal/channel/`.
- **`index.txt` ordering**: must use `Store.SnapshotOrdered()` (registration order), not `Snapshot()`.
- **`index.txt` status line**: `yp_name` が設定されている場合、末尾に YP ステータス行を追加（ID=all-zeros、Listeners/Relays=-9、Comment に Uptime）。

## Documentation

| 内容 | 場所 |
|---|---|
| システム構成・データフロー | `docs/architecture.md` |
| 設定リファレンス | `docs/configuration.md` |
| HTTP API 仕様 | `docs/HTTP_API.md` |
| DBスキーマ | `docs/database.md` |
| PCP プロトコル仕様 | `docs/protocol/PCP_SPEC.md` |
| YP 登録フロー詳細 | `docs/protocol/yp_channel_registration.md` |
| プレイヤー連携仕様（index.txt・URL導出） | `docs/protocol/player.md` |
| ジャンルフォーマット | `docs/protocol/genre.md` |
| チャンネル識別と統計の設計方針 | `docs/design/channel_identity.md` |
| 設計意思決定・参考資料 | `docs/design/` |
