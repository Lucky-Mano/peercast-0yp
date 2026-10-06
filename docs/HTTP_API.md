# HTTP API 仕様

すべてのエンドポイントは `/yp/` プレフィックス配下に置かれている。

## エンドポイント一覧

### `GET /yp/index.txt`

PeerCast プレイヤーが読み込むチャンネルリスト。YP4G 互換フォーマット。

フォーマット仕様（19フィールド `<>` 区切り）・プレイヤーによる URL 導出ルールは
[protocol/player.md](protocol/player.md) を参照。

---

### `GET /yp/api/config`

フロントエンド SPA が起動時に読み込むサーバ設定。

**レスポンス（JSON）:**

```json
{
  "ypIndexURL": "https://example.com/yp/index.txt",
  "pcpAddress": "pcp://example.com/",
  "genrePrefix": "yp"
}
```

`genrePrefix` は `[pcp] genre_prefix` の値（既定 `yp`）。使い方ページのジャンル設定の案内に使う。

---

### `GET /yp/getgmt.php?cn={channel_name}`

PeerCast プレイヤーが開く統計ページ URL。SPA が `/channels/{name}` にリダイレクトして処理する。

| パラメータ | 内容 |
|---|---|
| `cn` | チャンネル名（URL エンコード） |

---

### `GET /yp/chat.php?cn={channel_name}`

PeerCast プレイヤーが開くチャットページ URL。SPA が「チャット機能を提供していません」ページを表示する。

| パラメータ | 内容 |
|---|---|
| `cn` | チャンネル名（URL エンコード） |

---

### `GET /yp/api/channels`

現在放送中のチャンネル一覧をJSONで返す。

**レスポンス（JSON配列）:**

```json
[
  {
    "id": "0102...",
    "name": "チャンネル名",
    "genre": "Music",
    "desc": "説明",
    "url": "http://...",
    "comment": "コメント",
    "bitrate": 128,
    "contentType": "MP3",
    "track": {
      "title": "曲名",
      "artist": "アーティスト",
      "album": "アルバム",
      "contact": "http://..."
    },
    "tracker": {
      "ip": "1.2.3.4",
      "port": 7144,
      "firewalled": false
    },
    "numListeners": 5,
    "numRelays": 2,
    "upTime": 3600
  }
]
```

---

### `GET /yp/api/history?limit={n}&offset={n}`

過去の配信セッション一覧。

| パラメータ | デフォルト | 最大 |
|---|---|---|
| `limit` | 50 | 200 |
| `offset` | 0 | — |

**レスポンス（JSON配列）:**

```json
[
  {
    "id": 42,
    "channelName": "チャンネル名",
    "contentType": "FLV",
    "genre": "Music",
    "description": "説明",
    "url": "http://...",
    "comment": "コメント",
    "startedAt": "2026-03-07T20:00:00+09:00",
    "endedAt": "2026-03-07T22:30:00+09:00",
    "durationMin": 150
  }
]
```

`endedAt` は配信中の場合 `null`。過去7日分を `startedAt DESC` で返す。

---

### `GET /yp/api/channels/activity?name={channel_name}`

指定チャンネルの過去365日間の日別放送時間。

| パラメータ | 内容 |
|---|---|
| `name` | チャンネル名 |

**レスポンス（JSON配列）:**

```json
[
  { "date": "2026-03-07", "minutes": 150 },
  { "date": "2026-03-06", "minutes": 60 }
]
```

`date` 昇順。放送のなかった日は含まれない。

---

### `GET /yp/api/channels/timeline?name={channel_name}&date={YYYYMMDD}`

指定チャンネルの特定日のスナップショット履歴（10分刻み）。

| パラメータ | 内容 |
|---|---|
| `name` | チャンネル名 |
| `date` | 日付（`YYYYMMDD` 形式） |

**レスポンス（JSON配列）:**

```json
[
  {
    "recordedAt": "2026-03-07T20:00:00+09:00",
    "listeners": 5,
    "relays": 2,
    "changed": true,
    "name": "チャンネル名",
    "genre": "Music",
    "description": "説明",
    "url": "http://...",
    "comment": "コメント",
    "trackTitle": "曲名",
    "trackArtist": "アーティスト"
  },
  {
    "recordedAt": "2026-03-07T20:01:00+09:00",
    "listeners": 7,
    "relays": 2,
    "changed": false
  }
]
```

`changed: false` の行はリスナー数・リレー数のみ。`changed: true` の行はメタデータに変化があった時点で、変化内容のフィールドも含む（`omitempty`）。
