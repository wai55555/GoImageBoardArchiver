// Package adapter は、サイト固有の処理を抽象化するインターフェースと、
// その具体的な実装を提供します。これにより、GIBAは様々なウェブサイトに
// プラグイン形式で対応できます。
package adapter

import (
	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"
)

// SiteAdapter は、サイト固有の処理を抽象化するインターフェースです。
type SiteAdapter interface {
	// Prepare は、HTTPリクエストの前にサイト固有の準備（Cookie設定など）を行います。
	Prepare(client *network.Client, taskConfig config.Task) error
	// BuildCatalogURL は、掲示板のベースURLからカタログページの完全なURLを構築します。
	BuildCatalogURL(baseURL string) (string, error)
	ParseCatalog(htmlBody []byte) ([]model.ThreadInfo, error)
	// ParseThreadHTML は、スレッドHTMLを解析可能な形式（通常はUTF-8文字列）に変換します。
	ParseThreadHTML(htmlBody []byte) (string, error)
	// ExtractMediaFiles は、HTMLコンテンツからメディアファイル情報を抽出します。
	ExtractMediaFiles(htmlContent string, threadURL string) ([]model.MediaInfo, error)
	// ReconstructHTML は、HTMLコンテンツ内のリンクをローカルパスに書き換えます。
	ReconstructHTML(htmlContent string, thread model.ThreadInfo, mediaFiles []model.MediaInfo) (string, error)
}

// BoardScopedCatalogParser は、カタログ URL (board を含む) を引数で受け取って
// カタログを解析できるアダプタの任意インターフェースです。
//
// SiteAdapter 自体は変更しません。board をアダプタの可変フィールドに保持すると
// 監視/手動実行の並行時に上書きされ得るため、board は呼び出しごとに
// catalogURL から導出し、引数として渡します。
type BoardScopedCatalogParser interface {
	// ParseCatalogWithBoard は catalogURL から board を導出して解析します。
	ParseCatalogWithBoard(catalogURL string, body []byte) ([]model.ThreadInfo, error)
}

// ParseCatalogWithBoard は、アダプタが BoardScopedCatalogParser を実装していれば
// それを呼び、未実装なら従来の SiteAdapter.ParseCatalog にフォールバックします。
// カタログ取得の全経路 (監視 / 手動実行 / --cli) はこの関数を通してください。
func ParseCatalogWithBoard(siteAdapter SiteAdapter, catalogURL string, body []byte) ([]model.ThreadInfo, error) {
	if p, ok := siteAdapter.(BoardScopedCatalogParser); ok {
		return p.ParseCatalogWithBoard(catalogURL, body)
	}
	return siteAdapter.ParseCatalog(body)
}
