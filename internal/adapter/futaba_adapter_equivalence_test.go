package adapter

// Phase 2 (U-1) の等価性テスト。
//
// 入力はテストコード内の埋め込み定数で持つ (testdata/ は .gitignore 除外中で
// fresh clone に存在しないため、Phase 2 の等価性テストをそこへ依存させない)。
// 期待値は、差し替え前の正規表現版 (efa4df4 の futaba_adapter.go) で
// 同一入力から記録した抽出結果である。

import (
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"
)

// カタログの埋め込みフィクスチャ (UTF-8。テスト内で Shift_JIS に変換して渡す)。
// 実サイトの HTML ではなく、ふたばのカタログ構造 (td > a[href=res/*.htm] + small)
// を再現した人工データ。
const phase2CatalogHTML = `<!DOCTYPE html>
<html><head><meta charset="Shift_JIS"><title>カタログ</title></head><body>
<div id="cattable"><table><tr>
<td class="thre"><a href="res/1234567890123.htm"><img src="thumb/1234567890123s.jpg" alt="1234567890123"></a><small>長いタイトル<br>の<b>テスト</b> &amp; 記号 (10)</small></td>
<td class="thre"><a href="res/9876543210987.htm"><img src="thumb/9876543210987s.jpg"></a><small>通常スレッド (5)</small></td>
<td class="thre"><a href="./res/5555555555555.htm"><img src="thumb/5555555555555s.jpg"></a><small></small></td>
<td class="thre"><a href="res/1234567890123.htm"><img src="thumb/1234567890123s.jpg"></a><small>重複リンク (1)</small></td>
<td class="thre"><a href="/b/">板トップ</a></td>
</tr></table></div>
</body></html>`

// スレッドの埋め込みフィクスチャ (UTF-8)。
// 重複リンク、jpg/png/mp4/webm、引用符なし href、桁数不足、メディアでないリンクを含む。
const phase2ThreadHTML = `<!DOCTYPE html>
<html><head><meta charset="Shift_JIS"><title>スレッド</title></head><body>
<div class="thre"><table><tr><td>
<a href="/b/src/1700000000123.jpg">1700000000123.jpg</a> (123 KB)<br>
<a href="/b/src/1700000000123.jpg"><img src="/b/thumb/1700000000123s.jpg" alt="1700000000123"></a><br>
<a href='/b/src/1700000000456.png'>1700000000456.png</a><br>
<a href=/b/src/1700000000789.mp4>1700000000789.mp4</a><br>
<a href="/b/src/1700000000999.webm">動画</a><br>
<a href="/b/src/170000004.gif">桁数不足</a><br>
<a href="/b/">板トップ</a><br>
</td></tr></table></div>
</body></html>`

const phase2ThreadURL = "http://may.2chan.net/b/res/123456789.htm"

// toShiftJIS は、埋め込み定数 (UTF-8) をふたばの実配信と同じ Shift_JIS に変換します。
func toShiftJIS(t *testing.T, utf8HTML string) []byte {
	t.Helper()
	b, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte(utf8HTML))
	if err != nil {
		t.Fatalf("Shift_JIS への変換に失敗しました: %v", err)
	}
	return b
}

// TestParseCatalog_GoQueryEquivalence は、goquery 版のカタログ解析が
// 正規表現版と同一の結果を返すことを検証します (REQ-2.1 / REQ-2.3)。
func TestParseCatalog_GoQueryEquivalence(t *testing.T) {
	t.Parallel()

	adapter := NewFutabaAdapter()
	threads, err := adapter.ParseCatalog(toShiftJIS(t, phase2CatalogHTML))
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
	content, err := adapter.ParseThreadHTML(toShiftJIS(t, phase2ThreadHTML))
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
