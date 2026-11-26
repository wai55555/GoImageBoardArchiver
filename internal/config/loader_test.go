package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadingWithFutabaSettings(t *testing.T) {
	// 1. Arrange (準備)
	testConfigPath := filepath.Join("testdata", "test_config.json")
	data, err := os.ReadFile(testConfigPath)
	if err != nil {
		t.Fatalf("テスト設定ファイル '%s' の読み込みに失敗しました: %v", testConfigPath, err)
	}

	// 2. Act (実行)
	// エクスポートされた ParseAndResolve を直接呼び出す
	cfg, err := ParseAndResolve(data)
	if err != nil {
		t.Fatalf("ParseAndResolveで予期せぬエラーが発生しました: %v", err)
	}

	// 3. Assert (検証)
	// タスク総数の検証
	if len(cfg.Tasks) != 3 {
		t.Fatalf("タスクの総数が期待値と異なります。期待値: 3, 実際値: %d", len(cfg.Tasks))
	}

	// --- 'Task With Settings'の検証 (テンプレートの継承) ---
	task1 := cfg.Tasks[0]
	if task1.TaskName != "Task With Settings" {
		t.Errorf("タスク1の名前が不正です: %s", task1.TaskName)
	}
	if task1.FutabaCatalogSettings == nil {
		t.Fatal("タスク1: FutabaCatalogSettingsがnilであってはなりません。")
	}
	if task1.FutabaCatalogSettings.Cols != 9 {
		t.Errorf("タスク1: Colsが期待値と異なります。期待値: 9, 実際値: %d", task1.FutabaCatalogSettings.Cols)
	}
	if task1.FutabaCatalogSettings.Rows != 100 {
		t.Errorf("タスク1: Rowsが期待値と異なります。期待値: 100, 実際値: %d", task1.FutabaCatalogSettings.Rows)
	}
	if task1.FutabaCatalogSettings.TitleLength != 20 {
		t.Errorf("タスク1: TitleLengthが期待値と異なります。期待値: 20, 実際値: %d", task1.FutabaCatalogSettings.TitleLength)
	}

	// --- 'Task Without Settings'の検証 (設定なし) ---
	task2 := cfg.Tasks[1]
	if task2.TaskName != "Task Without Settings" {
		t.Errorf("タスク2の名前が不正です: %s", task2.TaskName)
	}
	if task2.FutabaCatalogSettings != nil {
		t.Errorf("タスク2: FutabaCatalogSettingsはnilであるべきです。実際値: %+v", *task2.FutabaCatalogSettings)
	}

	// --- 'Task With Override'の検証 (上書き) ---
	task3 := cfg.Tasks[2]
	if task3.TaskName != "Task With Override" {
		t.Errorf("タスク3の名前が不正です: %s", task3.TaskName)
	}
	if task3.FutabaCatalogSettings == nil {
		t.Fatal("タスク3: FutabaCatalogSettingsがnilであってはなりません。")
	}
	if task3.FutabaCatalogSettings.Cols != 10 {
		t.Errorf("タスク3: Colsが期待値と異なります。期待値: 10, 実際値: %d", task3.FutabaCatalogSettings.Cols)
	}
	if task3.FutabaCatalogSettings.Rows != 50 {
		t.Errorf("タスク3: Rowsが期待値と異なります。期待値: 50, 実際値: %d", task3.FutabaCatalogSettings.Rows)
	}
	if task3.FutabaCatalogSettings.TitleLength != 30 {
		t.Errorf("タスク3: TitleLengthが期待値と異なります。期待値: 30, 実際値: %d", task3.FutabaCatalogSettings.TitleLength)
	}
}

// TestParseAndResolveNormalizesUndefinedStrings は、Web UI の旧バグが書き込む
// リテラル "undefined" を、読み込み時に未設定（空文字）として扱うことを検証します
// (REQ-1.3(b))。対象フィールドは列挙で閉じており、キーワード系は対象外です。
func TestParseAndResolveNormalizesUndefinedStrings(t *testing.T) {
	t.Parallel()

	// フィクスチャファイルは使わず、テスト内の埋め込み文字列で完結させます
	// (internal/adapter/testdata のような未追跡依存を作らないため)。
	const cfgJSON = `{
  "config_version": "1.0",
  "log_file_path": "undefined",
  "notification_webhook_url": "undefined",
  "tasks": [
    {
      "task_name": "undefined",
      "site_adapter": "undefined",
      "target_board_url": "undefined",
      "save_root_directory": "undefined",
      "search_keyword": "undefined",
      "search_keywords": ["undefined", "AI"],
      "exclude_keywords": ["undefined"],
      "post_content_filters": {
        "include_any_text": ["undefined"],
        "exclude_all_text": ["undefined"],
        "include_author_ids": ["undefined"]
      }
    },
    {
      "task_name": "通常タスク",
      "site_adapter": "futaba",
      "target_board_url": "https://may.2chan.net/b/",
      "save_root_directory": "./downloads/"
    }
  ]
}`

	cfg, err := ParseAndResolve([]byte(cfgJSON))
	if err != nil {
		t.Fatalf("ParseAndResolveで予期せぬエラーが発生しました: %v", err)
	}
	if len(cfg.Tasks) != 2 {
		t.Fatalf("タスクの総数が期待値と異なります。期待値: 2, 実際値: %d", len(cfg.Tasks))
	}

	if cfg.LogFilePath != "" {
		t.Errorf("log_file_path は空文字になるべきです。実際値: %q", cfg.LogFilePath)
	}
	if cfg.NotificationWebhookURL != "" {
		t.Errorf("notification_webhook_url は空文字になるべきです。実際値: %q", cfg.NotificationWebhookURL)
	}

	task := cfg.Tasks[0]
	for name, got := range map[string]string{
		"task_name":           task.TaskName,
		"site_adapter":        task.SiteAdapter,
		"target_board_url":    task.TargetBoardURL,
		"save_root_directory": task.SaveRootDirectory,
	} {
		if got != "" {
			t.Errorf("%s は空文字（未設定）になるべきです。実際値: %q", name, got)
		}
	}

	// キーワード系は対象外: リテラル "undefined" は正当な検索語として保持される。
	if task.SearchKeyword != "undefined" {
		t.Errorf("search_keyword は保持されるべきです。実際値: %q", task.SearchKeyword)
	}
	if len(task.SearchKeywords) != 2 || task.SearchKeywords[0] != "undefined" || task.SearchKeywords[1] != "AI" {
		t.Errorf("search_keywords は保持されるべきです。実際値: %v", task.SearchKeywords)
	}
	if len(task.ExcludeKeywords) != 1 || task.ExcludeKeywords[0] != "undefined" {
		t.Errorf("exclude_keywords は保持されるべきです。実際値: %v", task.ExcludeKeywords)
	}
	if task.PostContentFilters == nil {
		t.Fatal("post_content_filters は保持されるべきです")
	}
	if len(task.PostContentFilters.IncludeAnyText) != 1 || task.PostContentFilters.IncludeAnyText[0] != "undefined" {
		t.Errorf("post_content_filters.include_any_text は保持されるべきです。実際値: %v", task.PostContentFilters.IncludeAnyText)
	}
	if len(task.PostContentFilters.ExcludeAllText) != 1 || task.PostContentFilters.ExcludeAllText[0] != "undefined" {
		t.Errorf("post_content_filters.exclude_all_text は保持されるべきです。実際値: %v", task.PostContentFilters.ExcludeAllText)
	}
	if len(task.PostContentFilters.IncludeAuthorIDs) != 1 || task.PostContentFilters.IncludeAuthorIDs[0] != "undefined" {
		t.Errorf("post_content_filters.include_author_ids は保持されるべきです。実際値: %v", task.PostContentFilters.IncludeAuthorIDs)
	}

	// 通常の値はそのまま保持される (正規化が過剰に効いていないことの確認)。
	normal := cfg.Tasks[1]
	if normal.TaskName != "通常タスク" || normal.SiteAdapter != "futaba" ||
		normal.TargetBoardURL != "https://may.2chan.net/b/" || normal.SaveRootDirectory != "./downloads/" {
		t.Errorf("通常の値が変更されています: %+v", normal)
	}
}
