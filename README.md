# GIBA (Go Image Board Archiver)

日々の日課をもっと自動化したくてつくりました。
画像掲示板のスレッドを自動的にアーカイブするシステムトレイアプリケーション。
ふたばちゃんねるに加え、4chan（公式 JSON API）に対応しています。

## 特徴

- **システムトレイ常駐** - バックグラウンドで動作し、タスクバーから操作可能
- **増分アーカイブ** - スレッドの更新を検知して自動的に新しいレスと画像を保存
- **削除レス保存** - 削除されたレスも完全版HTMLに保存
- **監視モード** - 定期的にカタログをチェックして自動アーカイブ
- **フィルタリング** - キーワード、メディア数、投稿内容で対象スレッドを絞り込み
- **レジューム機能** - ダウンロード中断時も途中から再開可能
- **修復機能** - 破損・欠損したファイルを検知して再ダウンロード
- **完全なオフライン閲覧** - HTML、CSS、画像を含む自己完結型アーカイブ
- **Web UI で設定編集** - 内蔵の設定画面（127.0.0.1、動的ポート）で編集し、保存すると再起動なしで反映
- **大文字小文字を区別しないキーワード検索** - 単一 / OR / 除外のすべてで `lora` = `Lora` = `LORA`
- **4chan 対応** - 公式 JSON API (`a.4cdn.org`) を使用。4chan ではメタデータが消える
  AI 画像スレ向けに、本文中の **catbox** (`files.catbox.moe` / `litter.catbox.moe`) の
  メディアも保存する

## インストール

### 必要要件

- Go 1.21以上
- Windows/macOS/Linux

### ビルド

付属のビルドスクリプトを使用すると簡単にビルドできます。

```bash
# Windows
build.bat
```

または手動でビルド:

```bash
go mod tidy
go build -ldflags="-s -w" -o giba.exe ./cmd/giba
```

## 使い方

### 1. 設定ファイルの作成

`config.json`を編集して、アーカイブしたいスレッドの条件を設定します。

```json
{
  "config_version": "1.0",
  "global_save_root_directory": "./downloads/",
  "global_max_concurrent_tasks": 4,
  "safety_stop_min_disk_gb": 1.0,
  "network": {
    "user_agent": "Mozilla/5.0 ...",
    "per_domain_interval_ms": {
      "may.2chan.net": 1000
    },
    "request_timeout_ms": 30000
  },
  "notification_webhook_url": "",
  "enable_log_file": false,
  "log_file_path": "",
  "task_templates": {
    "futaba_default": {
      "site_adapter": "futaba",
      "save_root_directory": "./downloads/"
    }
  },
  "tasks": [
    {
      "enabled": true,
      "task_name": "Futaba AI",
      "site_adapter": "futaba",
      "target_board_url": "https://may.2chan.net/b/",
      "search_keywords": ["AI", "猫"],
      "minimum_media_count": 5,
      "watch_interval_ms": 900000
    }
  ]
}
```

- `retry_count` / `retry_wait_ms` / `request_timeout_ms` は**タスク側**の設定です
  (`network` 側にあるのは `user_agent` / `default_headers` /
  `per_domain_interval_ms` / `request_timeout_ms` の 4 つ)。
- `search_keywords` は OR 検索です。空の場合は後方互換の `search_keyword` (単一) が使われます。
- `minimum_media_count` は OP 添付を含むメディア総数、`watch_interval_ms` は監視間隔（ミリ秒）です。
- `task_templates` を使うと共通設定を継承できます (`use_template` で指定)。
- 4chan タスクは `"site_adapter": "4chan"`、`"target_board_url": "https://boards.4chan.org/<board>/"`
  (SFW 板は `boards.4channel.org` も可) を指定します。スレッドは
  `https://a.4cdn.org/<board>/thread/<no>.json` から取得し、メディアは
  `i.4cdn.org` から保存します。API のマナー (合算 1req/秒以下、同一スレッドは 10 秒以上)
  はクライアント側で自動的に守られます。

### 2. アプリケーションの起動

```bash
# システムトレイモード（デフォルト）
./giba.exe

# CLIモード（1回だけ実行）
./giba.exe --cli

# 監視モード（CLI）
./giba.exe --watch

# 検証・修復モード
./giba.exe --verify --repair
```

### 3. システムトレイから操作

- **監視モードを有効にする** - 自動的に定期チェックを開始
- **今すぐ全タスクを実行** - 手動で即座に実行
- **保存先フォルダを開く** - アーカイブされたファイルを確認
- **ログと設定** - 最新ログの表示、設定画面（Web UI）を開く
- **ログファイルに出力 / コンソールを表示** - 実行中のログ出力を切り替え
- **一時停止 / 再開** - 実行中の活動を一時停止

### 4. Web UI で設定を編集

トレイメニューの「ログと設定」→「設定画面を開く」で、内蔵 Web UI が開きます
(127.0.0.1 の空きポートを自動で使用)。タスクの追加・削除・編集ができ、保存すると
`config.json` にアトミック書き込みされ、実行中のアプリへ即時反映されます (hot-reload)。
実行中のタスクは開始時点の設定で完走し、新しい設定は次回実行から効きます。

## アーカイブ構造

```
downloads/
└── 2025-11/
    └── 1234567890_スレ名/
        ├── index.htm              # 最新状態のHTML
        ├── archive_full.html      # 削除レスを含む完全版
        ├── .snapshot.json         # 増分判定用（メディア数・最終更新）
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
| `enabled` | タスクの有効/無効 | `true` |
| `search_keywords` | スレタイ検索キーワード（OR、大小文字非区別） | `["AI", "猫"]` |
| `search_keyword` | 単一キーワード（後方互換。`search_keywords` が空のときのみ使用） | `"AI"` |
| `exclude_keywords` | 除外キーワード（大小文字非区別） | `["NG", "spam"]` |
| `minimum_media_count` | 最小メディア数（OP 添付を含む） | `5` |
| `watch_interval_ms` | 監視間隔（ミリ秒） | `900000` (15分) |
| `save_root_directory` | 保存先（空ならグローバル設定） | `"./downloads/"` |
| `retry_count` / `retry_wait_ms` | ダウンロード再試行回数 / 待機ミリ秒 | `3` / `5000` |
| `use_template` | 継承する `task_templates` のキー | `"futaba_default"` |

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

- `watch_interval_ms` が設定されているか確認
- ログファイル（`giba_YYYY-MM-DD.log`）でエラーを確認
- システムトレイから「監視モードを有効にする」をクリック

### ダウンロードが失敗する

- ネットワーク設定を確認（`request_timeout_ms`, `retry_count`）
- レート制限を調整（`per_domain_interval_ms`）
- ディスク容量を確認

## ライセンス

MIT License

## 開発

### プロジェクト構造

```
GoImageBoardArchiver/
├── cmd/giba/              # エントリーポイント（トレイ / CLI / 検証モード）
├── internal/
│   ├── adapter/           # サイト固有のロジック（ふたば=goquery / 4chan=JSON API）
│   │   └── testdata/      # 人工フィクスチャ（コミット対象）
│   ├── config/            # 設定の読み込み・解決・検証
│   ├── core/              # コアロジック（実行・ダウンロード・増分・削除レス・検証）
│   ├── model/             # データモデル
│   ├── network/           # HTTP通信・レート制限・Cookie
│   ├── systray/           # システムトレイUI と司令塔ループ
│   └── webui/             # 内蔵 Web UI（設定編集、embed で同梱）
├── css/                   # アーカイブに同梱する静的ファイル
├── dist/                  # 検証用ビルドの置き場（git 管理外）
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
- [goquery](https://github.com/PuerkitoBio/goquery) - カタログ / スレッドの HTML 解析
- [golang.org/x/time](https://pkg.go.dev/golang.org/x/time/rate) - ドメイン別レート制限
- [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) - Shift_JIS 変換
