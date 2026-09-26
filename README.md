# 令和のYP 0yp（れいわいぴー）

**peercast-0yp** は、既存の PeerCast ルートサーバに依存しない、自己完結型の YP（Yellow Page）サーバです。
Go で実装されており、PCP（PeerCast Protocol）バイナリプロトコルによるチャンネル登録の受付から
チャンネルリストの配信・放送履歴の公開まで、YP として必要な機能をすべて内包しています。

## 機能

- **独立した PCP ルートサーバ**
  外部の PeerCast ルートサーバに依存せず、TCP/7144 で PCP バイナリプロトコルを直接処理します。
- **チャンネルリスト配信**
  PeerCast プレイヤー向けに `index.txt`（YP4G 互換フォーマット）を配信します。
- **放送履歴ページ**
  配信セッションやリスナー数の推移を記録し、他の YP と同様に放送履歴を閲覧できるページを提供します。

## クイックスタート

Compose の責務は次のとおりです。

- `docker-compose.yml`: app のビルド、TOML マウント、PCP 公開。
- `docker-compose.dev.yml`: 開発用 Caddy と PostgreSQL、app の DB 接続先。
- `docker-compose.prod.yml`: app 単体からホストのPostgreSQLへ接続する設定。Caddy は含みません。
- VPS 本番全体: 別リポジトリ `yayaue.me/compose.yaml` が Caddy と app を起動します。本リポジトリの Compose と同時起動しないでください。

```bash
cp .env.example .env
# .env の DB 認証情報を編集。ローカル開発では SITE_DOMAIN=localhost に設定
# peercast-0yp.toml の公開 URL も環境に合わせる

docker compose -f docker-compose.yml -f docker-compose.dev.yml config -q
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
docker compose -f docker-compose.yml -f docker-compose.dev.yml logs -f app
docker compose -f docker-compose.yml -f docker-compose.dev.yml down
```

開発用 Caddy は HTTP_PORT（既定例: 80）、443/TCP・UDP、app は PCP_PORT（既定: 7144）を公開します。
HTTP の `/yp/index.txt` は直接配信し、その他は HTTPS へ転送します。
HTTPS の `/yp` と `/yp/*` は app、その他は `public/` を配信します。
`localhost` の HTTPS は Caddy のローカル CA を信頼する必要があります。
PostgreSQL は named volume に永続化します。`down -v` はデータも削除するため通常の停止には使いません。
開発では app は `postgres:5432` へ接続します。PostgreSQL のホスト公開ポートは `POSTGRES_PORT` で変更できます。

### 本番

`/srv/yayaue.me` と `/srv/peercast-0yp` に配置し、`yayaue.me/README.md` の移行・起動手順を使ってください。
本番 DB 認証情報は `/srv/yayaue.me/.env` に設定します。
本番の静的トップページと Caddy 設定も `yayaue.me` で管理します。このリポジトリの `public/` と Caddyfile は開発用です。

ホスト DB に接続する app 単体を起動する場合だけ、次を使用できます（HTTP はホストに公開されません）。

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml config -q
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

### 設定

アプリ内部の待受ポートは `.env` ではなく `peercast-0yp.toml` で設定します。
既存 Compose は `[http] port = 80`、`[pcp] port = 7144` を前提にしています。
公開 URL (`yp_url`, `yp_index_url`, `pcp_address`) も環境に合わせて設定してください。
`YP_PATH` は Caddy の振り分け設定だけなので、現在のアプリ・フロントエンドに合わせて `/yp` を維持してください。
TOML 変更後は `restart app`、環境変数変更後は `up -d app` で反映します。

## 開発

```bash
go build ./...
go test ./...
go vet ./...
```

### フロントエンドの開発サーバ

```bash
cd web && npm run dev
```

`http://localhost:5173/yp/` で起動します。APIリクエストは `http://localhost:8080` にプロキシされます。

### フロントエンドの変更を反映する

フロントエンド（`web/`）は `go:embed` でGoバイナリに埋め込まれます。
変更を反映するにはフロントエンドをビルドしてからDockerイメージを再ビルドしてください。

```bash
cd web && npm run build && cd ..
docker compose -f docker-compose.yml -f docker-compose.dev.yml build app
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d app
```

## ドキュメント

→ [docs/index.md](docs/index.md)

## License

This project is licensed under the GNU General Public License v3.0. Portions of this software are Copyright (C) 2026 ITAGAKI Takayuki
