package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
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
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "futaba_thread_normal.html"))
	if err != nil {
		t.Fatalf("テスト用のHTMLファイルの読み込みに失敗しました: %v", err)
	}
	utf8Reader := transform.NewReader(strings.NewReader(string(htmlContent)), japanese.ShiftJIS.NewDecoder())
	doc, err := goquery.NewDocumentFromReader(utf8Reader)
	if err != nil {
		t.Fatalf("goqueryドキュメントの生成に失敗しました: %v", err)
	}
	adapter := NewFutabaAdapter()

	// Act
	mediaFiles, err := adapter.ExtractMediaFiles(doc)

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
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "futaba_thread_normal.html"))
	if err != nil {
		t.Fatalf("テスト用のHTMLファイルの読み込みに失敗しました: %v", err)
	}
	utf8Reader := transform.NewReader(strings.NewReader(string(htmlContent)), japanese.ShiftJIS.NewDecoder())
	doc, _ := goquery.NewDocumentFromReader(utf8Reader)

	adapter := NewFutabaAdapter()
	originalMediaFiles, _ := adapter.ExtractMediaFiles(doc)
	if len(originalMediaFiles) == 0 {
		t.Fatal("テストデータからメディアファイルが抽出できませんでした。")
	}

	// ダミーのローカルパスを持つ新しいスライスを作成
	var mediaFilesWithLocalPath []MediaInfo
	for _, mf := range originalMediaFiles { // 'i' を '_' に変更
		newMf := mf
		newMf.LocalPath = fmt.Sprintf("./media/%s", mf.OriginalFilename)
		mediaFilesWithLocalPath = append(mediaFilesWithLocalPath, newMf)
	}

	// Act
	reconstructedHTML, err := adapter.ReconstructHTML(doc, mediaFilesWithLocalPath)

	// Assert
	if err != nil {
		t.Fatalf("ReconstructHTMLが予期せぬエラーを返しました: %v", err)
	}

	// 最初のメディアファイルについて、元のURLが消え、ローカルパスに置き換わっていることを検証
	firstOriginalURL := originalMediaFiles[0].URL
	if strings.Contains(reconstructedHTML, firstOriginalURL) {
		t.Errorf("再構成後のHTMLに、元の外部リンク '%s'が残ってしまっています", firstOriginalURL)
	}
	firstLocalPath := mediaFilesWithLocalPath[0].LocalPath
	if !strings.Contains(reconstructedHTML, firstLocalPath) {
		t.Errorf("再構成後のHTMLに、期待されるローカルパス '%s'が含まれていません", firstLocalPath)
	}
}

// --- Test for ExtractMediaFiles_EdgeCases ---

func TestFutabaAdapter_ExtractMediaFiles_EdgeCases(t *testing.T) {
	// Arrange
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "futaba_thread_edge_cases.html"))
	if err != nil {
		t.Fatalf("テスト用のHTMLファイルの読み込みに失敗しました: %v", err)
	}
	utf8Reader := transform.NewReader(strings.NewReader(string(htmlContent)), japanese.ShiftJIS.NewDecoder())
	doc, err := goquery.NewDocumentFromReader(utf8Reader)
	if err != nil {
		t.Fatalf("goqueryドキュメントの生成に失敗しました: %v", err)
	}
	adapter := NewFutabaAdapter()

	// Act
	mediaFiles, err := adapter.ExtractMediaFiles(doc)

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
