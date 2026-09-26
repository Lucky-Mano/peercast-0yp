# システムアーキテクチャ

## 概要

`peercast-0yp` は単一バイナリで動作する PeerCast YP（Yellow Page）サーバです。
PCP ルートサーバ・HTTP サーバ・アーカイブ記録器を同一プロセスで統合しています。

## コンポーネント構成

```
main.go
  ├── channel.Store          ← 共有インメモリ状態
  ├── pcp.Server             ← PCP ルートサーバ (port 7144)
  ├── archive.Recorder       ← PostgreSQL へのアーカイブ記録
  └── httpd.Server           ← HTTP サーバ (port 80)
```

### channel.Store (`internal/channel/`)

チャンネル情報とヒット（ノード）のインメモリレジストリ。すべての操作はスレッドセーフ。

| メソッド | 説明 |
|---|---|
| `AddHit(info, hit)` | チャンネル情報とホスト情報を登録・更新 |
| `DelHit(chanID, sessionID)` | ヒットを削除（配信終了時） |
| `RemoveDeadHits(timeout)` | 一定時間更新のないヒットを自動削除 |
| `SnapshotOrdered()` | 登録順のチャンネル一覧スナップショット（archive.Recorder 用） |
| `Snapshot()` | チャンネル全量スナップショット（HTTP API 用） |

### pcp.Server (`internal/pcp/`)

TCP/7144 で PCP バイナリプロトコルを処理するルートサーバ。

**接続ごとのライフサイクル:**

```
Step 1: pcp\n アトムを受信（接続開始確認）
Step 2: helo アトムを受信 → agent, sid, port, ver をパース
Step 3: oleh アトムを送信（サーバ情報・クライアントのリモート IP を返す）
Step 4: root アトムを送信（更新間隔・最小バージョン等の設定情報）
Step 5: バリデーション（バージョン・セッション ID・重複チェック）
Step 6: ok(0) + root > upd を送信 → クライアントが即座にチャンネル情報を送信
Step 7: 送信ゴルーチン（sendLoop）を起動
Step 8: 読み取りループ: bcst → Store を更新; quit → 切断
```

**サーバ全体のバックグラウンドタスク:**

| タスク | 間隔 | 内容 |
|---|---|---|
| cleanupLoop | 500 ms | `RemoveDeadHits` で古いヒットを削除 |
| broadcastLoop | 120 秒（設定可能） | 全 CIN セッションに `bcst > root > upd` を送信 |

### archive.Recorder (`internal/archive/`)

`channel.Store` をポーリングし、配信セッションとスナップショットを PostgreSQL に記録する。

- **ポーリング間隔**: 1 秒（セッション開始・終了の検出）
- **スナップショット記録**: 10 分間隔で `channel_snapshots` に INSERT
- **起動時処理**: `ended_at = NULL` のレコードを `NOW()` でクローズ（クラッシュリカバリ）

### httpd.Server (`internal/httpd/`)

chi ベースの HTTP サーバ。

| エンドポイント | データソース | 用途 |
|---|---|---|
| `GET /yp/index.txt` | channel.Store | PeerCast プレイヤー向けチャンネルリスト |
| `GET /yp/api/config` | TOML config | フロントエンド向けサーバ設定（YP URL・PCP アドレス） |
| `GET /yp/api/channels` | channel.Store | 現在放送中チャンネル（JSON） |
| `GET /yp/api/history` | PostgreSQL | 過去の配信セッション一覧 |
| `GET /yp/api/channels/activity` | PostgreSQL | チャンネル別配信頻度（芝生グラフ用） |
| `GET /yp/api/channels/timeline` | PostgreSQL | 特定日のスナップショット履歴 |
| `GET /yp/*` | embed（SPA） | React フロントエンド（静的ファイル） |

各エンドポイントの詳細は [HTTP_API.md](HTTP_API.md) を参照。詳細なプロトコル仕様は [protocol/yp_channel_registration.md](protocol/yp_channel_registration.md) を参照。

---

## データフロー

```
PeerCast クライアント              Web ブラウザ
  │  PCP (TCP/7144)                │  HTTPS (TCP/443) or HTTP (TCP/80)
  ▼                                ▼
pcp.Server                      Caddy (リバースプロキシ)
  │ AddHit / DelHit                │ /yp/* → app:80
  ▼                                │ /     → 静的ファイル
channel.Store               httpd.Server (TCP/80)
  │                            ├── /yp/index.txt      → channel.Store
  │ SnapshotOrdered() × 1s     ├── /yp/api/channels   → channel.Store
  ▼                            ├── /yp/api/config      → TOML config
archive.Recorder               ├── /yp/api/history    → PostgreSQL
  │ INSERT / UPDATE             ├── /yp/api/channels/* → PostgreSQL
  ▼                            └── /yp/*              → SPA (embed)
PostgreSQL
  ├── channel_sessions    ← セッション開始・終了時刻
  └── channel_snapshots   ← 10分間隔リスナー数スナップショット
```

---

## データモデル

### インメモリ（channel.Store）

```
Store
  └── map[GnuID]*HitList
        ├── Info（チャンネルメタデータ）
        │    ├── ID, BroadcastID, Name
        │    ├── Bitrate, ContentType, Genre, Desc, URL, Comment
        │    └── Track（Title, Artist, Album, Contact）
        ├── LastHitTime
        └── []Hit（ノード一覧）
              ├── SessionID, ChanID
              ├── GlobalAddr, LocalAddr
              ├── NumListeners, NumRelays, UpTime
              └── Tracker, Relay, Direct, Firewalled, Recv, CIN（flags）
```

### 永続化（PostgreSQL）

詳細は [database.md](database.md) を参照。

| テーブル | 内容 |
|---|---|
| `channel_sessions` | 配信セッション（開始・終了時刻、基本メタデータ） |
| `channel_snapshots` | 10分間隔スナップショット（リスナー数・全メタデータ） |

---

## 設計上の選択

| 項目 | 決定内容 | 理由 |
|---|---|---|
| 同一バイナリ | PCP サーバ・HTTP サーバを同一プロセス | Store を直接参照できる。単一マシン運用のためスケールアウト不要 |
| アーカイブ方式 | ポーリング差分（1 秒） | Store への変更不要で疎結合。スナップショットは 1 分間隔のため精度は十分 |
| BCID 不変性 | 初回登録後変更不可 | チャンネル「所有権」の簡易検証（C++ 実装踏襲） |
| セッションごとのゴルーチン | 書き込みは sendLoop、読み取りはメインゴルーチン | 書き込みを直列化しつつ読み取りをブロックさせない |
