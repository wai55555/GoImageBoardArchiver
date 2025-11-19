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
