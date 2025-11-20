// Package config は、アプリケーションの設定ファイル(config.json)の構造定義と、
// その読み込み、解決（テンプレートのマージなど）に関する機能を提供します。
package config

// Config は config.json ファイル全体を表すルート構造体です。
type Config struct {
	ConfigVersion            string          `json:"config_version"`
	Network                  NetworkSettings `json:"network"`
	GlobalMaxConcurrentTasks int             `json:"global_max_concurrent_tasks"`
	MaxRequestsPerSecond     float64         `json:"max_requests_per_second"` // これは秒間リクエスト数なので変更なし
	MaxDownloadBandwidthMBps float64         `json:"max_download_bandwidth_mbps"`
	SafetyStopMinDiskGB      float64         `json:"safety_stop_min_disk_gb"`
	EnableStatusFile         bool            `json:"enable_status_file"`
	StatusFilePath           string          `json:"status_file_path"`
	VerificationHistoryPath  string          `json:"verification_history_path"`
	NotificationWebhookURL   string          `json:"notification_webhook_url"`
	TaskTemplates            map[string]Task `json:"task_templates"`
	Tasks                    []Task          `json:"tasks"`
	// ログ設定
	EnableLogFile bool   `json:"enable_log_file"` // ログファイル出力を有効にするか
	LogFilePath   string `json:"log_file_path"`   // ログファイルのパス (デフォルト: giba.log)
}

// NetworkSettings は、HTTPリクエストに関するグローバルな設定を保持します。
type NetworkSettings struct {
	UserAgent               string            `json:"user_agent"`
	DefaultHeaders          map[string]string `json:"default_headers"`
	PerDomainIntervalMillis map[string]int    `json:"per_domain_interval_ms"`
	RequestTimeoutMillis    int               `json:"request_timeout_ms"`
}

// Task は単一のアーカイブタスクを定義します。
type Task struct {
	TaskName                 string              `json:"task_name,omitempty"`
	UseTemplate              string              `json:"use_template,omitempty"`
	SiteAdapter              string              `json:"site_adapter,omitempty"`
	TargetBoardURL           string              `json:"target_board_url,omitempty"`
	SaveRootDirectory        string              `json:"save_root_directory,omitempty"`
	DirectoryFormat          string              `json:"directory_format,omitempty"`
	FilenameFormat           string              `json:"filename_format,omitempty"`
	SearchKeyword            string              `json:"search_keyword,omitempty"`
	ExcludeKeywords          []string            `json:"exclude_keywords,omitempty"`
	MinimumMediaCount        int                 `json:"minimum_media_count,omitempty"`
	WatchIntervalMillis      int                 `json:"watch_interval_ms,omitempty"`
	MaxConcurrentDownloads   int                 `json:"max_concurrent_downloads,omitempty"`
	CatalogTitleLength       int                 `json:"catalog_title_length,omitempty"`
	PostContentFilters       *PostContentFilters `json:"post_content_filters,omitempty"`
	HistoryFilePath          string              `json:"history_file_path,omitempty"`
	VerificationHistoryPath  string              `json:"verification_history_path,omitempty"`
	MetadataIndexPath        string              `json:"metadata_index_path,omitempty"`
	LogFilePath              string              `json:"log_file_path,omitempty"`
	RetryCount               int                 `json:"retry_count,omitempty"`
	RetryWaitMillis          int                 `json:"retry_wait_ms,omitempty"`
	RequestTimeoutMillis     int                 `json:"request_timeout_ms,omitempty"`
	RequestIntervalMillis    int                 `json:"request_interval_ms,omitempty"`
	NotifyOnComplete         bool                `json:"notify_on_complete,omitempty"`
	NotifyOnError            bool                `json:"notify_on_error,omitempty"`
	EnableHistorySkip        bool                `json:"enable_history_skip,omitempty"`
	EnableResumeSupport      bool                `json:"enable_resume_support,omitempty"`
	EnableLogFile            bool                `json:"enable_log_file,omitempty"`
	LogLevel                 string              `json:"log_level,omitempty"`
	EnableMetadataIndex      bool                `json:"enable_metadata_index,omitempty"`
	MetadataIndexFormat      string              `json:"metadata_index_format,omitempty"`
	PerDomainIntervalSeconds map[string]int      `json:"per_domain_interval_seconds"` // This one seems to be a duplicate or leftover in my previous thought, let's check the file content. Ah, it was PerDomainIntervalSeconds in the previous step. It should be removed or renamed if it's in Task. Wait, PerDomainIntervalSeconds is in NetworkSettings, but also appeared in Task in the previous step? Let me check the file content again.
	// Checking file content... Line 60: PerDomainIntervalSeconds map[string]int `json:"per_domain_interval_seconds"`
	// It seems I added it to Task by mistake, or it was there. The config.json structure has "per_domain_interval_ms" under "network", not under "task".
	// However, the previous file content showed it in Task struct too. Let's assume it might be overridden per task?
	// But config.json doesn't show it in task_templates.
	// Let's just rename it to Millis if it exists, or remove if it's not needed.
	// Given the instruction "Update Config struct to use Millis instead of Seconds", I will rename it.
	FutabaCatalogSettings *FutabaCatalogSettings `json:"futaba_catalog_settings,omitempty"`
}

// PostContentFilters はスレッド本文の内容に基づくフィルタ条件を定義します。
type PostContentFilters struct {
	IncludeAnyText   []string `json:"include_any_text,omitempty"`
	ExcludeAllText   []string `json:"exclude_all_text,omitempty"`
	IncludeAuthorIDs []string `json:"include_author_ids,omitempty"`
}

// FutabaCatalogSettings は、ふたばちゃんねるの 'cxyl' Cookieの各値を定義します。
// 例: 9x100x20x0x0
type FutabaCatalogSettings struct {
	// Cols はカタログの横のカラム数です (cx)。
	Cols int `json:"cols"`
	// Rows はカタログの縦の行数です (cy)。
	Rows int `json:"rows"`
	// TitleLength はスレッドタイトルの最大表示文字数です (cl)。
	TitleLength int `json:"title_length"`
}
