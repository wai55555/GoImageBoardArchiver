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
