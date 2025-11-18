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
