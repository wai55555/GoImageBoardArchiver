# GIBA (Go Image Board Archiver)

日々の日課をもっと自動化したくてつくりました。
画像掲示板のスレッドを自動的にアーカイブするシステムトレイアプリケーション。
現在はふたばちゃんねる専用。暇ができたら4chan対応もするかも？

## 特徴

- **システムトレイ常駐** - バックグラウンドで動作し、タスクバーから操作可能
- **増分アーカイブ** - スレッドの更新を検知して自動的に新しいレスと画像を保存
- **削除レス保存** - 削除されたレスも完全版HTMLに保存
- **監視モード** - 定期的にカタログをチェックして自動アーカイブ
- **フィルタリング** - キーワード、メディア数、投稿内容で対象スレッドを絞り込み
- **レジューム機能** - ダウンロード中断時も途中から再開可能
- **完全なオフライン閲覧** - HTML、CSS、画像を含む自己完結型アーカイブ

## インストール

### 必要要件

- Go 1.21以上
- Windows/macOS/Linux



## 使い方

### 1. 設定ファイルの作成

`config.json`を編集して、アーカイブしたいスレッドの条件を設定します。

```json
{
  "config_version": "1.0",
  "network": {
    "request_timeout_ms": 30000,
    "retry_count": 3,
    "retry_wait_ms": 5000,
    "rate_limit_requests_per_second": 1
  },
  "tasks": [
    {
      "task_name": "Futaba AI",
      "site_adapter": "futaba",
      "target_board_url": "https://may.2chan.net/b/",
      "search_keyword": "AI",
      "minimum_media_count": 5,
      "watch_interval_millis": 900000
    }
  ]
}
```

### 2. アプリケーションの起動

```bash
# システムトレイモード（デフォルト）
./giba.exe

# CLIモード（1回だけ実行）
./giba.exe --cli

# 監視モード（CLI）
./giba.exe --watch
```

### 3. システムトレイから操作

- **監視モードを有効にする** - 自動的に定期チェックを開始
- **今すぐ全タスクを実行** - 手動で即座に実行
- **保存先フォルダを開く** - アーカイブされたファイルを確認

## アーカイブ構造

```
downloads/
└── 2025-11/
    └── 1234567890_スレ名/
        ├── index.htm              # 最新状態のHTML
        ├── archive_full.html      # 削除レスを含む完全版
        ├── css/
        │   └── futaba.css
        ├── img/                   # フルサイズ画像
        │   ├── 1234567890.jpg
        │   └── 1234567891.webm
        └── thumb/                 # サムネイル
            ├── 1234567890s.jpg
            └── 1234567891s.jpg
```

## 設定項目

### タスク設定

| 項目 | 説明 | 例 |
|------|------|-----|
| `task_name` | タスクの識別名 | `"Futaba AI"` |
| `site_adapter` | サイトアダプタ | `"futaba"` |
| `target_board_url` | 対象板のURL | `"https://may.2chan.net/b/"` |
| `search_keyword` | スレタイ検索キーワード | `"AI"` |
| `exclude_keywords` | 除外キーワード | `["NG", "spam"]` |
| `minimum_media_count` | 最小メディア数 | `5` |
| `watch_interval_millis` | 監視間隔（ミリ秒） | `900000` (15分) |

### フィルタリング

```json
{
  "post_content_filters": {
    "include_any_text": ["キーワード1", "キーワード2"],
    "exclude_all_text": ["NGワード"]
  }
}
```

## 増分アーカイブの仕組み

1. **初回アーカイブ** - スレッドの全レスと画像を保存
2. **スナップショット作成** - `.snapshot.json`にメディア数を記録
3. **定期チェック** - 監視モードで定期的にカタログを確認
4. **更新検知** - メディア数が増えていれば再アーカイブ
5. **削除検知** - 前回のHTMLと比較して削除されたレスを検出
6. **完全版保存** - `archive_full.html`に削除レスも含めて保存

## トラブルシューティング

### アイコンが表示されない

Windowsの場合、ICO形式のアイコンが必要です。以下のコマンドでアイコンを再生成できます：

```bash
python scripts/generate_icons.py > internal/systray/icon/icon.go
```

### 監視モードが動作しない

- `watch_interval_millis`が設定されているか確認
- ログファイル（`giba.log`）でエラーを確認
- システムトレイから「監視モードを有効にする」をクリック

### ダウンロードが失敗する

- ネットワーク設定を確認（`request_timeout_ms`, `retry_count`）
- レート制限を調整（`rate_limit_requests_per_second`）
- ディスク容量を確認

## ライセンス

MIT License

## 開発

### プロジェクト構造

```
GoImageBoardArchiver/
├── cmd/giba/              # エントリーポイント
├── internal/
│   ├── adapter/           # サイト固有のロジック
│   ├── config/            # 設定管理
│   ├── core/              # コアロジック
│   ├── model/             # データモデル
│   ├── network/           # HTTP通信
│   └── systray/           # システムトレイUI
├── css/                   # 静的ファイル
└── config.json            # 設定ファイル
```

### 新しいサイトアダプタの追加

1. `internal/adapter/`に新しいアダプタファイルを作成
2. `SiteAdapter`インターフェースを実装
3. `factory.go`にアダプタを登録

```go
type SiteAdapter interface {
    Prepare(client *network.Client, task config.Task) error
    BuildCatalogURL(boardURL string) (string, error)
    ParseCatalog(html []byte) ([]model.ThreadInfo, error)
    ParseThreadHTML(html []byte) (string, error)
    ExtractMediaFiles(htmlContent, threadURL string) ([]model.MediaInfo, error)
    ReconstructHTML(htmlContent string, thread model.ThreadInfo, mediaFiles []model.MediaInfo) (string, error)
}
```

## 貢献

プルリクエストを歓迎します！バグ報告や機能要望はIssueでお願いします。

## 謝辞

- [fyne.io/systray](https://github.com/fyne-io/systray) - システムトレイ機能
- [goquery](https://github.com/PuerkitoBio/goquery) - HTML解析
