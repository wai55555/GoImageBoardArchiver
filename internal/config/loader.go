package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// taskPatch は、タスク設定をデコードするための中間ヘルパー構造体です。
// ポインタ型を使用しているのは、JSONに存在しないフィールド（未設定）と、
// ゼロ値（例: 0や空文字列）が設定されているケースを区別するためです。
type taskPatch struct {
	Enabled                *bool                  `json:"enabled,omitempty"`
	TaskName               *string                `json:"task_name,omitempty"`
	UseTemplate            string                 `json:"use_template,omitempty"`
	SiteAdapter            *string                `json:"site_adapter,omitempty"`
	TargetBoardURL         *string                `json:"target_board_url,omitempty"`
	SaveRootDirectory      *string                `json:"save_root_directory,omitempty"`
	DirectoryFormat        *string                `json:"directory_format,omitempty"`
	FilenameFormat         *string                `json:"filename_format,omitempty"`
	SearchKeyword          *string                `json:"search_keyword,omitempty"`
	ExcludeKeywords        *[]string              `json:"exclude_keywords,omitempty"`
	MinimumMediaCount      *int                   `json:"minimum_media_count,omitempty"`
	WatchIntervalMillis    *int                   `json:"watch_interval_ms,omitempty"`
	MaxConcurrentDownloads *int                   `json:"max_concurrent_downloads,omitempty"`
	PostContentFilters     *PostContentFilters    `json:"post_content_filters,omitempty"`
	RetryCount             *int                   `json:"retry_count,omitempty"`
	RetryWaitMillis        *int                   `json:"retry_wait_ms,omitempty"`
	RequestTimeoutMillis   *int                   `json:"request_timeout_ms,omitempty"`
	RequestIntervalMillis  *int                   `json:"request_interval_ms,omitempty"`
	NotifyOnComplete       *bool                  `json:"notify_on_complete,omitempty"`
	NotifyOnError          *bool                  `json:"notify_on_error,omitempty"`
	EnableHistorySkip      *bool                  `json:"enable_history_skip,omitempty"`
	EnableResumeSupport    *bool                  `json:"enable_resume_support,omitempty"`
	EnableLogFile          *bool                  `json:"enable_log_file,omitempty"`
	LogLevel               *string                `json:"log_level,omitempty"`
	EnableMetadataIndex    *bool                  `json:"enable_metadata_index,omitempty"`
	FutabaCatalogSettings  *FutabaCatalogSettings `json:"futaba_catalog_settings,omitempty"`
}

// rawConfig は、設定ファイルをデコードするための中間構造体です。
type rawConfig struct {
	ConfigVersion           string          `json:"config_version"`
	GlobalSaveRootDirectory string          `json:"global_save_root_directory,omitempty"`
	WebUITheme              string          `json:"web_ui_theme,omitempty"`
	Network                 NetworkSettings `json:"network"`
	GlobalMaxConcurrentTasks int            `json:"global_max_concurrent_tasks"`
	SafetyStopMinDiskGB     float64         `json:"safety_stop_min_disk_gb"`
	NotificationWebhookURL  string          `json:"notification_webhook_url"`
	TaskTemplates           map[string]Task `json:"task_templates"`
	Tasks                   []taskPatch     `json:"tasks"`
	EnableLogFile           bool            `json:"enable_log_file"`
	LogFilePath             string          `json:"log_file_path,omitempty"`
}

// LoadAndResolve は、指定されたパスから設定ファイルを読み込み、解析と解決を行います。
func LoadAndResolve(path string) (*Config, error) {
	absPath, _ := filepath.Abs(path)
	cwd, _ := os.Getwd()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("設定ファイル '%s' の読み込みに失敗しました (Abs: '%s', Cwd: '%s'): %w", path, absPath, cwd, err)
	}
	return ParseAndResolve(data)
}

// ParseAndResolve は、設定データのバイトスライスを解析し、テンプレートを解決して最終的な設定を返します。
func ParseAndResolve(data []byte) (*Config, error) {
	var rawCfg rawConfig
	if err := json.Unmarshal(data, &rawCfg); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError

		if errors.As(err, &syntaxErr) {
			line, col := computeLineAndColumn(data, syntaxErr.Offset)
			return nil, fmt.Errorf("設定ファイルのJSON構文エラー (行 %d, 列 %d): %w", line, col, err)
		}
		if errors.As(err, &typeErr) {
			line, col := computeLineAndColumn(data, typeErr.Offset)
			return nil, fmt.Errorf("設定ファイルの型エラー (行 %d, 列 %d, フィールド '%s'): 期待値 %v, 実際 %v - %w",
				line, col, typeErr.Field, typeErr.Type, typeErr.Value, err)
		}
		return nil, fmt.Errorf("設定ファイルの解析に失敗しました: %w", err)
	}

	const compatibleVersion = "1.0"
	if rawCfg.ConfigVersion != compatibleVersion {
		// 今後のバージョニング対応を見据え、現在は警告に留めるか、厳格にエラーとするか選択。今回はエラーとする。
		return nil, fmt.Errorf("サポートされていない設定バージョン '%s' です。'%s' が必要です。", rawCfg.ConfigVersion, compatibleVersion)
	}

	// 新しいConfig構造体に合わせて初期化
	resolvedConfig := &Config{
		ConfigVersion:           rawCfg.ConfigVersion,
		GlobalSaveRootDirectory: rawCfg.GlobalSaveRootDirectory,
		WebUITheme:              rawCfg.WebUITheme,
		Network:                 rawCfg.Network,
		GlobalMaxConcurrentTasks: rawCfg.GlobalMaxConcurrentTasks,
		SafetyStopMinDiskGB:     rawCfg.SafetyStopMinDiskGB,
		NotificationWebhookURL:  rawCfg.NotificationWebhookURL,
		TaskTemplates:           rawCfg.TaskTemplates,
		EnableLogFile:           rawCfg.EnableLogFile,
		LogFilePath:             rawCfg.LogFilePath,
		Tasks:                   make([]Task, 0, len(rawCfg.Tasks)),
	}

	for _, patch := range rawCfg.Tasks {
		var resolvedTask Task
		if patch.UseTemplate != "" {
			template, ok := rawCfg.TaskTemplates[patch.UseTemplate]
			if !ok {
				taskName := "unknown"
				if patch.TaskName != nil {
					taskName = *patch.TaskName
				}
				return nil, fmt.Errorf("タスク '%s' が未定義のテンプレート '%s' を使用しています", taskName, patch.UseTemplate)
			}
			resolvedTask = template // テンプレートをベースにする
		}
		applyPatch(&resolvedTask, &patch) // パッチで上書き

		// Enabledフィールドが未設定の場合、デフォルトでtrueにする
		if resolvedTask.Enabled == nil {
			defaultValue := true
			resolvedTask.Enabled = &defaultValue
		}

		resolvedConfig.Tasks = append(resolvedConfig.Tasks, resolvedTask)
	}

	return resolvedConfig, nil
}

// applyPatch は、patchの非nilフィールドをtargetに上書きします。
func applyPatch(target *Task, patch *taskPatch) {
	target.UseTemplate = patch.UseTemplate

	if patch.Enabled != nil {
		target.Enabled = patch.Enabled
	}
	if patch.TaskName != nil {
		target.TaskName = *patch.TaskName
	}
	if patch.SiteAdapter != nil {
		target.SiteAdapter = *patch.SiteAdapter
	}
	if patch.TargetBoardURL != nil {
		target.TargetBoardURL = *patch.TargetBoardURL
	}
	if patch.SaveRootDirectory != nil {
		target.SaveRootDirectory = *patch.SaveRootDirectory
	}
	if patch.DirectoryFormat != nil {
		target.DirectoryFormat = *patch.DirectoryFormat
	}
	if patch.FilenameFormat != nil {
		target.FilenameFormat = *patch.FilenameFormat
	}
	if patch.SearchKeyword != nil {
		target.SearchKeyword = *patch.SearchKeyword
	}
	if patch.ExcludeKeywords != nil {
		target.ExcludeKeywords = *patch.ExcludeKeywords
	}
	if patch.MinimumMediaCount != nil {
		target.MinimumMediaCount = *patch.MinimumMediaCount
	}
	if patch.WatchIntervalMillis != nil {
		target.WatchIntervalMillis = *patch.WatchIntervalMillis
	}
	if patch.MaxConcurrentDownloads != nil {
		target.MaxConcurrentDownloads = *patch.MaxConcurrentDownloads
	}
	if patch.PostContentFilters != nil {
		target.PostContentFilters = patch.PostContentFilters
	}
	if patch.RetryCount != nil {
		target.RetryCount = *patch.RetryCount
	}
	if patch.RetryWaitMillis != nil {
		target.RetryWaitMillis = *patch.RetryWaitMillis
	}
	if patch.RequestTimeoutMillis != nil {
		target.RequestTimeoutMillis = *patch.RequestTimeoutMillis
	}
	if patch.RequestIntervalMillis != nil {
		target.RequestIntervalMillis = *patch.RequestIntervalMillis
	}
	if patch.NotifyOnComplete != nil {
		target.NotifyOnComplete = *patch.NotifyOnComplete
	}
	if patch.NotifyOnError != nil {
		target.NotifyOnError = *patch.NotifyOnError
	}
	if patch.EnableHistorySkip != nil {
		target.EnableHistorySkip = *patch.EnableHistorySkip
	}
	if patch.EnableResumeSupport != nil {
		target.EnableResumeSupport = *patch.EnableResumeSupport
	}
	if patch.EnableLogFile != nil {
		target.EnableLogFile = *patch.EnableLogFile
	}
	if patch.LogLevel != nil {
		target.LogLevel = *patch.LogLevel
	}
	if patch.EnableMetadataIndex != nil {
		target.EnableMetadataIndex = *patch.EnableMetadataIndex
	}
	if patch.FutabaCatalogSettings != nil {
		target.FutabaCatalogSettings = patch.FutabaCatalogSettings
	}
}

// computeLineAndColumn は、バイトオフセットから行番号と列番号（1始まり）を計算します。
func computeLineAndColumn(data []byte, offset int64) (int, int) {
	if offset < 0 || int(offset) > len(data) {
		return 0, 0
	}
	line := 1
	lastLineStart := 0
	for i, b := range data {
		if int64(i) == offset {
			return line, i - lastLineStart + 1
		}
		if b == '\n' {
			line++
			lastLineStart = i + 1
		}
	}
	return line, int(offset) - lastLineStart + 1
}
