// Package adapter は、サイト固有の処理を抽象化するインターフェースと、
// その具体的な実装を提供します。これにより、GIBAは様々なウェブサイトに
// プラグイン形式で対応できます。
package adapter

import (
	"bytes"

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model" // 新しいmodelパッケージをインポート
	"GoImageBoardArchiver/internal/network"

	"github.com/PuerkitoBio/goquery"
)

// ThreadInfo は、カタログから抽出されたスレッドの基本情報を保持します。
// modelパッケージに移動したため、ここでは定義を削除
// type ThreadInfo struct {
// 	ID       string
// 	Title    string
// 	URL      string
// 	ResCount int
// 	Date     time.Time
// }

// MediaInfo は、スレッド内の単一メディアファイルに関する情報を保持します。
// modelパッケージに移動したため、ここでは定義を削除
// type MediaInfo struct {
// 	URL              string
// 	OriginalFilename string
// 	ResNumber        int
// 	LocalPath        string
// }

// SiteAdapter は、サイト固有の処理を抽象化するインターフェースです。
type SiteAdapter interface {
	// Prepare は、HTTPリクエストの前にサイト固有の準備（Cookie設定など）を行います。
	Prepare(client *network.Client, taskConfig config.Task) error
	// BuildCatalogURL は、掲示板のベースURLからカタログページの完全なURLを構築します。
	BuildCatalogURL(baseURL string) (string, error)
	ParseCatalog(htmlBody []byte) ([]model.ThreadInfo, error) // model.ThreadInfoを使用
	ParseThreadHTML(htmlBody []byte) (*goquery.Document, error)
	ExtractMediaFiles(doc *goquery.Document, threadURL string) ([]model.MediaInfo, error)                         // model.MediaInfoを使用
	ReconstructHTML(doc *goquery.Document, thread model.ThreadInfo, mediaFiles []model.MediaInfo) (string, error) // model.ThreadInfo, model.MediaInfoを使用
}

// NewDocumentFromBytes は、[]byteからgoquery.Documentを生成するヘルパー関数です。
func NewDocumentFromBytes(htmlBody []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(bytes.NewReader(htmlBody))
}
