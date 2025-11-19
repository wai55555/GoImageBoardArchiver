package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// taskPatch は、タスク設定をデコードするための中間ヘルパー構造体です。
type taskPatch struct {
	TaskName                *string                `json:"task_name,omitempty"`
	UseTemplate             string                 `json:"use_template,omitempty"`
	SiteAdapter             *string                `json:"site_adapter,omitempty"`
	TargetBoardURL          *string                `json:"target_board_url,omitempty"`
	SaveRootDirectory       *string                `json:"save_root_directory,omitempty"`
	DirectoryFormat         *string                `json:"directory_format,omitempty"`
	FilenameFormat          *string                `json:"filename_format,omitempty"`
	SearchKeyword           *string                `json:"search_keyword,omitempty"`
	ExcludeKeywords         *[]string              `json:"exclude_keywords,omitempty"`
	MinimumMediaCount       *int                   `json:"minimum_media_count,omitempty"`
	WatchIntervalMillis     *int                   `json:"watch_interval_ms,omitempty"`
	MaxConcurrentDownloads  *int                   `json:"max_concurrent_downloads,omitempty"`
	CatalogTitleLength      *int                   `json:"catalog_title_length,omitempty"`
	PostContentFilters      *PostContentFilters    `json:"post_content_filters,omitempty"`
	HistoryFilePath         *string                `json:"history_file_path,omitempty"`
	VerificationHistoryPath *string                `json:"verification_history_path,omitempty"`
	MetadataIndexPath       *string                `json:"metadata_index_path,omitempty"`
	LogFilePath             *string                `json:"log_file_path,omitempty"`
	RetryCount              *int                   `json:"retry_count,omitempty"`
	RetryWaitMillis         *int                   `json:"retry_wait_ms,omitempty"`
	RequestTimeoutMillis    *int                   `json:"request_timeout_ms,omitempty"`
	RequestIntervalMillis   *int                   `json:"request_interval_ms,omitempty"`
	NotifyOnComplete        *bool                  `json:"notify_on_complete,omitempty"`
	NotifyOnError           *bool                  `json:"notify_on_error,omitempty"`
	EnableHistorySkip       *bool                  `json:"enable_history_skip,omitempty"`
	EnableResumeSupport     *bool                  `json:"enable_resume_support,omitempty"`
	EnableLogFile           *bool                  `json:"enable_log_file,omitempty"`
	LogLevel                *string                `json:"log_level,omitempty"`
	EnableMetadataIndex     *bool                  `json:"enable_metadata_index,omitempty"`
	MetadataIndexFormat     *string                `json:"metadata_index_format,omitempty"`
	FutabaCatalogSettings   *FutabaCatalogSettings `json:"futaba_catalog_settings,omitempty"`
}

// rawConfig は、設定ファイルをデコードするための中間構造体です。
type rawConfig struct {
	ConfigVersion            string          `json:"config_version"`
	Network                  NetworkSettings `json:"network"`
	GlobalUserAgent          string          `json:"global_user_agent"` // 削除対象
	GlobalMaxConcurrentTasks int             `json:"global_max_concurrent_tasks"`
	MaxRequestsPerSecond     float64         `json:"max_requests_per_second"`
	MaxDownloadBandwidthMBps float64         `json:"max_download_bandwidth_mbps"`
	SafetyStopMinDiskGB      float64         `json:"safety_stop_min_disk_gb"`
	EnableStatusFile         bool            `json:"enable_status_file"`
	StatusFilePath           string          `json:"status_file_path"`
	NotificationWebhookURL   string          `json:"notification_webhook_url"`
	TaskTemplates            map[string]Task `json:"task_templates"`
	Tasks                    []taskPatch     `json:"tasks"`
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
// この関数はテストのために分離されています。
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
		return nil, fmt.Errorf("サポートされていない設定バージョン '%s' です。'%s' が必要です。", rawCfg.ConfigVersion, compatibleVersion)
	}

	resolvedConfig := &Config{
		ConfigVersion:            rawCfg.ConfigVersion,
		Network:                  rawCfg.Network,
		GlobalMaxConcurrentTasks: rawCfg.GlobalMaxConcurrentTasks,
		MaxRequestsPerSecond:     rawCfg.MaxRequestsPerSecond,
		MaxDownloadBandwidthMBps: rawCfg.MaxDownloadBandwidthMBps,
		SafetyStopMinDiskGB:      rawCfg.SafetyStopMinDiskGB,
		EnableStatusFile:         rawCfg.EnableStatusFile,
		StatusFilePath:           rawCfg.StatusFilePath,
		NotificationWebhookURL:   rawCfg.NotificationWebhookURL,
		TaskTemplates:            rawCfg.TaskTemplates,
		Tasks:                    make([]Task, 0, len(rawCfg.Tasks)),
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
			resolvedTask = template
		}
		applyPatch(&resolvedTask, &patch)
		resolvedConfig.Tasks = append(resolvedConfig.Tasks, resolvedTask)
	}

	return resolvedConfig, nil
}

// applyPatch は、patchの非nilフィールドをtargetに上書きします。
func applyPatch(target *Task, patch *taskPatch) {
	target.UseTemplate = patch.UseTemplate
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
	if patch.CatalogTitleLength != nil {
		target.CatalogTitleLength = *patch.CatalogTitleLength
	}
	if patch.PostContentFilters != nil {
		target.PostContentFilters = patch.PostContentFilters
	}
	if patch.HistoryFilePath != nil {
		target.HistoryFilePath = *patch.HistoryFilePath
	}
	if patch.VerificationHistoryPath != nil {
		target.VerificationHistoryPath = *patch.VerificationHistoryPath
	}
	if patch.MetadataIndexPath != nil {
		target.MetadataIndexPath = *patch.MetadataIndexPath
	}
	if patch.LogFilePath != nil {
		target.LogFilePath = *patch.LogFilePath
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
	if patch.MetadataIndexFormat != nil {
		target.MetadataIndexFormat = *patch.MetadataIndexFormat
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
