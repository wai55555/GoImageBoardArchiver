// Package model は、アプリケーション全体で共有されるデータ構造を定義します。
// これらの構造体は、アダプター、コアロジック、および永続化層の間で
// データを転送するために使用されます。
package model

import (
	"time"
)

// ThreadInfo は、カタログから抽出されたスレッドの基本情報を保持します。
type ThreadInfo struct {
	ID       string
	Title    string
	URL      string
	ResCount int
	Date     time.Time
	// SearchText は一次フィルタで照合するテキスト (sub+com など)。
	// 空の場合は Title で照合する (ふたばは常に空)。
	SearchText string
	// MediaCount はカタログ時点のメディア総数。0 は不明。
	// 0 より大きい場合、minimum_media_count の一次フィルタに使う (4chan)。
	MediaCount int
	// LastModified は取得元の最終更新時刻 (Unix 秒)。0 は不明。
	// 4chan は catalog の last_modified を設定する。ふたばは常に 0。
	LastModified int64
}

// MediaInfo は、スレッド内の単一メディアファイルに関する情報を保持します。
type MediaInfo struct {
	URL              string // フルサイズ
	ThumbnailURL     string // サムネイル
	OriginalFilename string
	ResNumber        int
	LocalPath        string
	LocalThumbPath   string
}
