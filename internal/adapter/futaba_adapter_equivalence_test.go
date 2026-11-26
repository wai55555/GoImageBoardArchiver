package adapter

// Phase 2 (U-1) の等価性テスト。
//
// 入力はテストコード内の埋め込み定数で持つ (testdata/ は .gitignore 除外中で
// fresh clone に存在しないため、Phase 2 の等価性テストをそこへ依存させない)。
// 期待値は、差し替え前の正規表現版 (efa4df4 の futaba_adapter.go) で
// 同一入力から記録した抽出結果である。

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// フィクスチャは internal/adapter/testdata/ に置く (Phase 4 で正式化)。
// いずれも実サイトの HTML ではなく、ふたばの構造を再現した人工データで、
// Shift_JIS で保存されている (ふたばの実配信と同じ文字コード)。
const (
	phase2CatalogFixture = "futaba_catalog_equivalence.html"
	phase2ThreadFixture  = "futaba_thread_equivalence.html"
)

const phase2ThreadURL = "http://may.2chan.net/b/res/123456789.htm"

// readFixture は testdata 配下のフィクスチャを読み込みます。
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("フィクスチャ '%s' の読み込みに失敗しました: %v", name, err)
	}
	return b
}

// TestParseCatalog_GoQueryEquivalence は、goquery 版のカタログ解析が
// 正規表現版と同一の結果を返すことを検証します (REQ-2.1 / REQ-2.3)。
func TestParseCatalog_GoQueryEquivalence(t *testing.T) {
	t.Parallel()

	adapter := NewFutabaAdapter()
	threads, err := adapter.ParseCatalog(readFixture(t, phase2CatalogFixture))
	if err != nil {
		t.Fatalf("ParseCatalog が予期せぬエラーを返しました: %v", err)
	}

	// 正規表現版の記録値 (ID の重複は最初の1件のみ、href は生のまま)。
	want := []struct {
		id    string
		title string
		url   string
	}{
		{"1234567890123", "長いタイトル のテスト &amp; 記号 (10)", "res/1234567890123.htm"},
		{"9876543210987", "通常スレッド (5)", "res/9876543210987.htm"},
		{"5555555555555", "Thread 5555555555555", "./res/5555555555555.htm"},
	}

	if len(threads) != len(want) {
		t.Fatalf("スレッド数が正規表現版と異なります: got %d, want %d (%+v)", len(threads), len(want), threads)
	}
	for i, w := range want {
		got := threads[i]
		if got.ID != w.id {
			t.Errorf("[%d] ID: got %q, want %q", i, got.ID, w.id)
		}
		if got.Title != w.title {
			t.Errorf("[%d] Title: got %q, want %q", i, got.Title, w.title)
		}
		if got.URL != w.url {
			t.Errorf("[%d] URL: got %q, want %q", i, got.URL, w.url)
		}
		if got.ResCount != 0 {
			t.Errorf("[%d] ResCount: got %d, want 0 (正規表現版と同じ)", i, got.ResCount)
		}
		if got.Date.IsZero() || time.Since(got.Date) > time.Minute {
			t.Errorf("[%d] Date が解析時刻になっていません: %v", i, got.Date)
		}
	}
}

// TestExtractMediaFiles_GoQueryEquivalence は、goquery 版のメディア抽出が
// 正規表現版と同一であることを検証します (REQ-2.1 / REQ-2.3)。
//
// 意図的な差分 (REQ-2.3 の列挙対象): 正規表現版は、引用符なしの href が
// 後続リンクの開始引用符を飲み込むため、その次のメディア (この入力では
// mp4 と webm) を取りこぼしていた。goquery 版は正しく4件を返す。
// この差分は「正規表現版の取りこぼしの修正」であり、他の項目 (URL・
// サムネイル導出・重複排除・並び順・ResNumber) は正規表現版と同一。
func TestExtractMediaFiles_GoQueryEquivalence(t *testing.T) {
	t.Parallel()

	adapter := NewFutabaAdapter()
	content, err := adapter.ParseThreadHTML(readFixture(t, phase2ThreadFixture))
	if err != nil {
		t.Fatalf("ParseThreadHTML が予期せぬエラーを返しました: %v", err)
	}

	media, err := adapter.ExtractMediaFiles(content, phase2ThreadURL)
	if err != nil {
		t.Fatalf("ExtractMediaFiles が予期せぬエラーを返しました: %v", err)
	}

	want := []struct {
		url   string
		thumb string
	}{
		{"http://may.2chan.net/b/src/1700000000123.jpg", "http://may.2chan.net/b/thumb/1700000000123s.jpg"},
		{"http://may.2chan.net/b/src/1700000000456.png", "http://may.2chan.net/b/thumb/1700000000456s.jpg"},
		{"http://may.2chan.net/b/src/1700000000789.mp4", "http://may.2chan.net/b/thumb/1700000000789s.jpg"},
		{"http://may.2chan.net/b/src/1700000000999.webm", "http://may.2chan.net/b/thumb/1700000000999s.jpg"},
	}

	if len(media) != len(want) {
		t.Fatalf("メディア数が想定と異なります: got %d, want %d (%+v)", len(media), len(want), media)
	}
	for i, w := range want {
		got := media[i]
		if got.URL != w.url {
			t.Errorf("[%d] URL: got %q, want %q", i, got.URL, w.url)
		}
		if got.ThumbnailURL != w.thumb {
			t.Errorf("[%d] ThumbnailURL: got %q, want %q", i, got.ThumbnailURL, w.thumb)
		}
		if got.ResNumber != 0 {
			t.Errorf("[%d] ResNumber: got %d, want 0 (正規表現版と同じ)", i, got.ResNumber)
		}
	}

	// 重複リンク (jpg が2回) は1件に、桁数不足 (170000004.gif) と
	// 非メディアリンク (/b/) は除外されることを明示的に確認する。
	counts := map[string]int{}
	for _, m := range media {
		counts[m.URL]++
	}
	if counts["http://may.2chan.net/b/src/1700000000123.jpg"] != 1 {
		t.Errorf("重複 URL が1件に集約されていません: %v", counts)
	}
	for _, excluded := range []string{
		"http://may.2chan.net/b/src/170000004.gif",
		"http://may.2chan.net/b/",
	} {
		if counts[excluded] != 0 {
			t.Errorf("抽出してはいけない URL が含まれています: %s", excluded)
		}
	}
}
