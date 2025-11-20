// Package core は、GIBAアプリケーションの中核となるビジネスロジックを実装します。
package core

import (
	"fmt"
	"time"
)

// AppState はアプリケーションの全体的な状態を表すenumです。
type AppState int

const (
	StateInitializing AppState = iota // 初期化中
	StateIdle                         // アイドル
	StateWatching                     // 監視中
	StatePreparing                    // 実行準備中
	StateRunning                      // 実行中
	StatePaused                       // 一時停止中
	StateError                        // エラー
)

// String は AppState を人間可読な文字列に変換します。
func (s AppState) String() string {
	switch s {
	case StateInitializing:
		return "初期化中"
	case StateIdle:
		return "アイドル"
	case StateWatching:
		return "監視中"
	case StatePreparing:
		return "準備中"
	case StateRunning:
		return "実行中"
	case StatePaused:
		return "一時停止中"
	case StateError:
		return "エラー"
	default:
		return "不明"
	}
}

// AppStatus はコアエンジンからUIへ渡されるアプリケーションの状態を表します。
type AppStatus struct {
	State        AppState // 現在の活動状態
	Detail       string   // 現在の活動に関する詳細な説明
	SessionInfo  string   // 今回のセッションでの統計情報
	IsWatching   bool     // 監視モードが有効かどうか
	IsRunning    bool     // いずれかのタスクが実行中かどうか
	IsPaused     bool     // アプリケーションが一時停止中かどうか
	HasError     bool     // 致命的なエラーが発生しているかどうか
	ConfigLoaded bool     // 設定ファイルが正常に読み込まれているか
}

// SessionStats はセッション統計情報を管理します。
type SessionStats struct {
	StartTime         time.Time // 起動時刻
	ThreadsArchived   int       // アーカイブしたスレッド数
	FilesDownloaded   int       // ダウンロードしたファイル数
	TotalBytesWritten int64     // 合計ダウンロードサイズ（バイト）
}

// FormatSessionInfo はセッション統計情報を文字列にフォーマットします。
func (s *SessionStats) FormatSessionInfo() string {
	uptime := time.Since(s.StartTime)
	hours := int(uptime.Hours())
	minutes := int(uptime.Minutes()) % 60

	// サイズをMB単位に変換
	sizeMB := float64(s.TotalBytesWritten) / (1024 * 1024)

	return fmt.Sprintf("起動: %dh%dm | スレッド: %d | ファイル: %d | %.1fMB",
		hours, minutes, s.ThreadsArchived, s.FilesDownloaded, sizeMB)
}
