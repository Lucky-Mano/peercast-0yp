# httpd サーバ 要件・アーキテクチャ設計

## 概要

`peercast-0yp`（PCPルートサーバ）を心臓部として、Web UIや
PeerCastプレイヤー向け出力・アーカイブ閲覧を提供する多機能 httpd サーバを
**同一バイナリ**として実装する。

---

## 機能要件

### 機能1: Web ブラウザ用 YP情報一覧
- 現在放送中のチャンネル一覧を表示する React SPA を提供する
- フロントエンド: React（SPAとしてビルドしたものを Go バイナリに埋め込む）
- データ取得: React → JSON API (`GET /api/channels`) → `channel.Store`

### 機能2: PeerCastプレイヤー用チャンネルリスト
- PeerCastプレイヤーが読み込む `index.txt` 形式のテキストを出力する
- React は関与しない（プレイヤーが直接 HTTP で取得するテキストエンドポイント）
- ソース: インメモリの `channel.Store`（リアルタイム）

### 機能3: アーカイブ・統計
- 過去の配信履歴・統計を閲覧できるページを React SPA として提供する
- フロントエンド: 機能1と同一 SPA 内のページ
- データ取得: React → JSON API (`GET /api/history` 等) → PostgreSQL
- 記録する情報:
  - 配信開始・終了時刻（`channel_sessions` テーブル）
  - 1分間隔のリスナー数スナップショット（`channel_snapshots` テーブル）
- チャンネルマスタは持たない。チャンネルID（GnuID）と名前は各テーブルに直接持つ

#### 機能3-a: チャンネル別 配信頻度グラフ（芝生表示）
- GitHub の Contribution Graph のように、日単位の配信有無・頻度をカレンダー状のヒートマップで表示する
- セルの色の濃淡で「その日の配信時間の合計（または配信回数）」を表現する
- 表示単位: 1セル = 1日、横軸 = 週、縦軸 = 曜日（GitHub 形式）
- データ取得: React → `GET /api/channels/{id}/activity` → PostgreSQL 集計 → JSON
- 集計値: **配信時間合計（分）** をセルの濃淡に使用する
- 日付またぎセッションは日付ごとに按分して計上する（例: 23:10〜01:30の配信 → 当日50分・翌日90分）
- 集計クエリは `channel_sessions` の `started_at`/`ended_at` と各日の境界（00:00）で切り分けて計算する
- 芝生グラフのセルをクリックすると、その日の詳細（機能3-b）を表示する

#### 機能3-b: 日別タイムライン表示
- 芝生グラフの日付セルを選択したときに、その日の配信内容をタイムライン形式で表示する
- 表示内容（1行 = 1スナップショット、ただし配信詳細が変化した行のみ明示）:
  - 時刻
  - リスナー数（合計 / 直接接続）
  - 配信詳細（チャンネル名・概要・コメントなど）— 前回から変化したときのみ表示、変化なしは省略
- 参考: YP4G の getgmt.php（10分間隔）
- データ取得: React → `GET /api/channels/{id}/timeline?date=YYYYMMDD` → `channel_snapshots` → JSON
- `channel_snapshots` には配信詳細の変化検出に必要なフィールド（名前・概要・コメント・トラック情報）も記録する
- 変化検出は `channel_snapshots` を PostgreSQL の `LAG()` 窓関数でスキャンして行う（変化専用テーブルは持たない）

---

## 非機能要件

- **デプロイ**: 常に単一マシン上で動作する。スケールアウト不要
- **プロセス**: PCPサーバ・HTTPサーバを同一バイナリ・同一プロセスで動かす
- **永続化**: PostgreSQL をアーカイブ・統計の永続化ストアとして使用する
- **リアルタイム状態**: `channel.Store`（インメモリ）を単一の信頼できる情報源とする

---

## アーキテクチャ

### 同一バイナリを選択した理由

| 観点 | 判断 |
|---|---|
| 機能1・2はStoreの直接参照で実装できる | 別プロセス化するとStore公開手段が必要になり複雑化する |
| PostgreSQLが外部にある | 歴史データの共有はDBが解決する。gRPCは不要 |
| 常に同一マシン | 分散の利点がない。gRPCのオーバーヘッドだけが残る |
| 将来の分離 | 要件が変わったとき（別マシン化・独立デプロイ）に分離を検討する |

### `channel.Store` の追加API

`archive.Recorder` のポーリングに必要な `Snapshot()` を追加する。

```go
// ChannelState はRecorder向けに集約したチャンネルの状態スナップショット。
type ChannelState struct {
    Info      Info
    Track     Track
    Hits      []Hit
    Listeners int  // 全Hitのリスナー数合計
    Relays     int  // 全Hitのリレー数合計
}

// Snapshot は現在Storeに存在する全チャンネルの状態を返す。
func (s *Store) Snapshot() []ChannelState
```

### コンポーネント構成

```
main.go
  ├── channel.Store          ← 共有インメモリ状態（PCPで更新、APIで参照）
  ├── pcp.Server          ← PCPルートサーバ (port 7144)
  │     └── channel.Store への AddHit / DelHit
  ├── archive.Recorder       ← Store の変化を観察し PostgreSQL に記録
  └── httpd.Server           ← HTTPサーバ (port 80)
        ├── GET /yp/api/channels → channel.Store 参照 → JSON（機能1用API）
        ├── GET /yp/api/history  → PostgreSQL 参照 → JSON（機能3用API）
        ├── GET /yp/index.txt    → channel.Store 参照 → index.txt（機能2）
        └── GET /yp/*            → React SPA の静的ファイル（機能1・3 UI）
```

React SPA のビルド成果物は `//go:embed` でバイナリに埋め込み、単一バイナリで完結させる。

### データフロー

```
PeerCastクライアント          Webブラウザ（React SPA）
  │  PCP (TCP)                  │  HTTP
  ▼                             ▼
pcp.Server             httpd.Server
  │ AddHit / DelHit         ├── GET /yp/api/channels → channel.Store（リアルタイム）
  ▼                         ├── GET /yp/api/history  → PostgreSQL（履歴）
channel.Store               └── GET /yp/index.txt    → channel.Store（機能2）
  │
  │ 変化通知（イベントまたはポーリング）
  ▼
archive.Recorder
  │ INSERT / UPDATE
  ▼
PostgreSQL
```

### archive.Recorder の実装方針

**B. ポーリング差分方式を採用する。**

- Recorder が一定間隔（1s）で `Store.Snapshot()` を取得し、前回との差分を検出
- Store への変更不要（疎結合）
- スナップショットは10分間隔のため、1s polling でセッション開始・終了の検出精度は十分

（却下）A. イベント通知方式: Store に `AddHit`/`DelHit` フックを追加する案。精度のメリットが小さい割に Store の変更コストがあるため不採用。

**起動時処理**: Recorder 起動時に `ended_at = NULL` のレコードを `NOW()` で埋める。
クラッシュ・再起動由来の未クローズセッションを正常に終了扱いにするため。
同じ `channel_id` で再配信が始まった場合は新規レコードが INSERT されるため問題なし。

---

## 決定事項まとめ

| 項目 | 決定内容 |
|---|---|
| HTTPサーバのポート番号 | **80** |
| `archive.Recorder` の実装方式 | **ポーリング差分**（1s間隔、Store変更なし） |
| PostgreSQLのスキーマ設計 | `design/schema.md` 参照 |
| React SPA のビルド・配置方法 | `go:embed` でバイナリ埋め込み |
| HTTPフレームワーク | **chi**（`net/http` 互換、軽量、パスパラメータ対応） |
| PostgreSQL接続設定 | **設定ファイル**。デプロイ方法は開発環境（Docker）が動いてから検討 |
| フロントエンドのビルドツール | **Vite**（Create React App は事実上メンテナンス停止） |
| CORS ポリシー | 本番: same-origin のため不要。開発時: `CORS_ALLOW_ORIGINS` env var で `localhost:5173` 等を許可 |
| JSON API の認証・認可 | **不要**（公開YPの読み取りAPIのため。管理機能追加時に再検討） |
| `index.txt` フォーマットの詳細仕様 | 下記「index.txt フォーマット仕様」参照 |

---

## index.txt フォーマット仕様

詳細仕様は [../protocol/player.md](../protocol/player.md) を参照。

### ソート順

`channel.Store` への登録順（`AddHit` された順）。

### Content-Type

`text/plain`

### レコード形式

各チャンネルが **1行**、フィールドは `<>` 区切り（全19フィールド）：

```
チャンネル名<>チャンネルID<>トラッカーIP<>コンタクトURL<>ジャンル<>概要<>リスナー数<>リレー数<>ビットレート<>コンテンツタイプ<>トラックアーティスト<>トラックアルバム<>トラックタイトル<>トラックコンタクト<>URLエンコード済みチャンネル名<>配信時間(H:MM)<>click<>コメント<>直接接続可否(0/1)
```

| # | フィールド | 備考 |
|---|---|---|
| 1 | チャンネル名 | |
| 2 | チャンネルID | 32文字 hex（GnuID） |
| 3 | トラッカーIP | |
| 4 | コンタクトURL | |
| 5 | ジャンル | `?` を含む場合はリスナー非表示 |
| 6 | 概要 | |
| 7 | リスナー数 | ジャンルに `?` があれば `-1` |
| 8 | リレー数 | ジャンルに `?` があれば `-1` |
| 9 | ビットレート | |
| 10 | コンテンツタイプ | 例: `FLV`, `MP3` |
| 11 | トラックアーティスト | |
| 12 | トラックアルバム | |
| 13 | トラックタイトル | |
| 14 | トラックコンタクト | |
| 15 | URLエンコード済みチャンネル名 | |
| 16 | 配信時間 | `H:MM` 形式 |
| 17 | click | （用途不明、互換性のため出力） |
| 18 | コメント | |
| 19 | 直接接続可否 | `0` or `1`。HitList 内のいずれかの Hit に `PCPHostFlags1Direct` が立っていれば `1` |

### 末尾のステータス行

チャンネルレコードの末尾に YP ステータス行を追加する（実装済み）。
`peercast-0yp.toml` の `[http]` セクションで `yp_name` を設定すると有効になる。
フォーマットは通常チャンネル行と同じ19フィールドで、ID=全ゼロ、Listeners/Relays=-9、Comment に稼働時間を出力する。
