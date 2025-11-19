package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"GoImageBoardArchiver/internal/model"
)

// --- Test for ParseCatalog ---

func TestFutabaAdapter_ParseCatalog(t *testing.T) {
	// Arrange
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "futaba_catalog_long_title.html"))
	if err != nil {
		t.Fatalf("テスト用のHTMLファイルの読み込みに失敗しました: %v", err)
	}
	adapter := NewFutabaAdapter()

	// Act
	threads, err := adapter.ParseCatalog(htmlContent)

	// Assert
	if err != nil {
		t.Fatalf("ParseCatalogが予期せぬエラーを返しました: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("スレッドが一つも抽出されませんでした。")
	}
}

// --- Test for ExtractMediaFiles ---

func TestFutabaAdapter_ExtractMediaFiles(t *testing.T) {
	// Arrange
	htmlBytes, err := os.ReadFile(filepath.Join("testdata", "futaba_thread_normal.html"))
	if err != nil {
		t.Fatalf("テスト用のHTMLファイルの読み込みに失敗しました: %v", err)
	}
	adapter := NewFutabaAdapter()

	htmlContent, err := adapter.ParseThreadHTML(htmlBytes)
	if err != nil {
		t.Fatalf("ParseThreadHTMLが失敗しました: %v", err)
	}

	// Act
	// ダミーのURLを渡す
	mediaFiles, err := adapter.ExtractMediaFiles(htmlContent, "http://may.2chan.net/b/res/123456789.htm")

	// Assert
	if err != nil {
		t.Fatalf("ExtractMediaFilesが予期せぬエラーを返しました: %v", err)
	}
	if len(mediaFiles) == 0 {
		t.Fatal("メディアファイルが一つも抽出されませんでした。")
	}
}

// --- Test for ReconstructHTML ---

func TestFutabaAdapter_ReconstructHTML(t *testing.T) {
	// Arrange
	htmlBytes, err := os.ReadFile(filepath.Join("testdata", "futaba_thread_normal.html"))
	if err != nil {
		t.Fatalf("テスト用のHTMLファイルの読み込みに失敗しました: %v", err)
	}
	adapter := NewFutabaAdapter()

	htmlContent, err := adapter.ParseThreadHTML(htmlBytes)
	if err != nil {
		t.Fatalf("ParseThreadHTMLが失敗しました: %v", err)
	}

	threadURL := "http://may.2chan.net/b/res/123456789.htm"
	originalMediaFiles, err := adapter.ExtractMediaFiles(htmlContent, threadURL)
	if err != nil {
		t.Fatalf("ExtractMediaFilesが失敗しました: %v", err)
	}
	if len(originalMediaFiles) == 0 {
		t.Fatal("テストデータからメディアファイルが抽出できませんでした。")
	}

	// ダミーのローカルパスを持つ新しいスライスを作成
	var mediaFilesWithLocalPath []model.MediaInfo
	for _, mf := range originalMediaFiles {
		newMf := mf
		newMf.LocalPath = fmt.Sprintf("./media/%s", mf.OriginalFilename)
		mediaFilesWithLocalPath = append(mediaFilesWithLocalPath, newMf)
	}

	threadInfo := model.ThreadInfo{
		ID:    "123456789",
		Title: "Test Thread",
		URL:   "res/123456789.htm",
		Date:  time.Now(),
	}

	// Act
	reconstructedHTML, err := adapter.ReconstructHTML(htmlContent, threadInfo, mediaFilesWithLocalPath)

	// Assert
	if err != nil {
		t.Fatalf("ReconstructHTMLが予期せぬエラーを返しました: %v", err)
	}

	// 最初のメディアファイルについて、元のURLが消え、ローカルパスに置き換わっていることを検証
	// 注: ReconstructHTMLの実装によってはURL全体ではなくファイル名部分で置換している可能性があるため、
	// 検証ロジックは実装に合わせて調整する。
	// 現在の実装では strings.ReplaceAll(htmlContent, mf.URL, targetPath) を行っている。

	firstOriginalURL := originalMediaFiles[0].URL
	if strings.Contains(reconstructedHTML, firstOriginalURL) {
		t.Errorf("再構成後のHTMLに、元の外部リンク '%s'が残ってしまっています", firstOriginalURL)
	}

	// 期待されるパスは img/filename
	expectedPath := filepath.ToSlash(filepath.Join("img", filepath.Base(mediaFilesWithLocalPath[0].LocalPath)))
	if !strings.Contains(reconstructedHTML, expectedPath) {
		t.Errorf("再構成後のHTMLに、期待されるローカルパス '%s'が含まれていません", expectedPath)
	}
}

// --- Test for ExtractMediaFiles_EdgeCases ---

func TestFutabaAdapter_ExtractMediaFiles_EdgeCases(t *testing.T) {
	// Arrange
	htmlBytes, err := os.ReadFile(filepath.Join("testdata", "futaba_thread_edge_cases.html"))
	if err != nil {
		t.Fatalf("テスト用のHTMLファイルの読み込みに失敗しました: %v", err)
	}
	adapter := NewFutabaAdapter()

	htmlContent, err := adapter.ParseThreadHTML(htmlBytes)
	if err != nil {
		t.Fatalf("ParseThreadHTMLが失敗しました: %v", err)
	}

	// Act
	mediaFiles, err := adapter.ExtractMediaFiles(htmlContent, "http://may.2chan.net/b/res/999999999.htm")

	// Assert
	if err != nil {
		t.Fatalf("ExtractMediaFilesが予期せぬエラーを返しました: %v", err)
	}
	if len(mediaFiles) == 0 {
		t.Fatal("メディアファイルが一つも抽出されませんでした。")
	}

	// 多様なファイルタイプが含まれていることを検証
	var foundJpg, foundPng, foundMp4 bool
	for _, mf := range mediaFiles {
		switch filepath.Ext(mf.OriginalFilename) {
		case ".jpg":
			foundJpg = true
		case ".png":
			foundPng = true
		case ".mp4":
			foundMp4 = true
		}
	}

	if !foundJpg {
		t.Error(".jpg ファイルが見つかりませんでした。")
	}
	if !foundPng {
		t.Error(".png ファイルが見つかりませんでした。")
	}
	if !foundMp4 {
		t.Error(".mp4 ファイルが見つかりませんでした。")
	}
}
