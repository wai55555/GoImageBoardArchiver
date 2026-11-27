package adapter

// 4chan アダプタ (Phase 6) のテスト。
// 入力は testdata/ の人工 JSON フィクスチャ (実投稿・実画像を含まない)。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
)

func readJSONFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("フィクスチャ '%s' の読み込みに失敗しました: %v", name, err)
	}
	return b
}

func TestFourChan_ParseCatalogWithBoard(t *testing.T) {
	t.Parallel()

	a := NewFourChanAdapter()
	body := readJSONFixture(t, "fourchan_catalog.json")
	threads, err := a.(BoardScopedCatalogParser).ParseCatalogWithBoard(
		"https://a.4cdn.org/vg/catalog.json", body)
	if err != nil {
		t.Fatalf("ParseCatalogWithBoard: %v", err)
	}

	if len(threads) != 3 {
		t.Fatalf("スレッド数: got %d, want 3 (重複 no は1件に集約)", len(threads))
	}

	first := threads[0]
	if first.ID != "1001" {
		t.Errorf("ID: got %q, want 1001", first.ID)
	}
	if first.Title != "AI画像スレ & テスト" {
		t.Errorf("Title: got %q, want %q", first.Title, "AI画像スレ & テスト")
	}
	// sub が無いスレは com の冒頭をタイトルにする (タグ・エンティティ处理后)。
	if threads[1].Title != "タイトル無しスレ 本文の冒頭がタイトルになります" {
		t.Errorf("Title(fallback): got %q", threads[1].Title)
	}
	if !strings.HasPrefix(threads[1].SearchText, "タイトル無しスレ") {
		t.Errorf("SearchText に com が含まれていません: %q", threads[1].SearchText)
	}

	for _, th := range threads {
		want := "https://a.4cdn.org/vg/thread/" + th.ID + ".json"
		if th.URL != want {
			t.Errorf("URL: got %q, want %q", th.URL, want)
		}
	}

	// images(3) + OP 添付(1) = 4。OP 添付が無いスレは images のまま。
	if first.MediaCount != 4 {
		t.Errorf("MediaCount: got %d, want 4", first.MediaCount)
	}
	if threads[1].MediaCount != 0 {
		t.Errorf("MediaCount(no attachment): got %d, want 0", threads[1].MediaCount)
	}
	if first.LastModified != 1700000100 {
		t.Errorf("LastModified: got %d, want 1700000100", first.LastModified)
	}
}

func TestFourChan_ParseCatalogWithoutBoardFails(t *testing.T) {
	t.Parallel()
	a := NewFourChanAdapter()
	if _, err := a.ParseCatalog(readJSONFixture(t, "fourchan_catalog.json")); err == nil {
		t.Fatal("board を特定できない ParseCatalog はエラーになるべきです")
	}
}

func TestFourChan_ExtractMediaFiles(t *testing.T) {
	t.Parallel()

	a := NewFourChanAdapter()
	content, err := a.ParseThreadHTML(readJSONFixture(t, "fourchan_thread.json"))
	if err != nil {
		t.Fatalf("ParseThreadHTML: %v", err)
	}
	media, err := a.ExtractMediaFiles(content, "https://a.4cdn.org/vg/thread/1001.json")
	if err != nil {
		t.Fatalf("ExtractMediaFiles: %v", err)
	}

	want := map[string]struct {
		thumb string
		res   int
		file  string
	}{
		"https://i.4cdn.org/vg/1700000000123.png": {"https://i.4cdn.org/vg/1700000000123s.jpg", 1001, "1700000000123.png"},
		"https://files.catbox.moe/abc123.png":     {"", 1001, "abc123.png"},
		"https://i.4cdn.org/vg/1700000000456.mp4": {"https://i.4cdn.org/vg/1700000000456s.jpg", 1002, "1700000000456.mp4"},
		"https://litter.catbox.moe/def456.mp4":    {"", 1002, "def456.mp4"},
	}
	if len(media) != len(want) {
		var got []string
		for _, m := range media {
			got = append(got, m.URL)
		}
		t.Fatalf("メディア数: got %d (%v), want %d", len(media), got, len(want))
	}
	for _, m := range media {
		w, ok := want[m.URL]
		if !ok {
			t.Errorf("想定外の URL: %s", m.URL)
			continue
		}
		if m.ThumbnailURL != w.thumb {
			t.Errorf("%s: ThumbnailURL got %q, want %q", m.URL, m.ThumbnailURL, w.thumb)
		}
		if m.ResNumber != w.res {
			t.Errorf("%s: ResNumber got %d, want %d", m.URL, m.ResNumber, w.res)
		}
		if m.OriginalFilename != w.file {
			t.Errorf("%s: OriginalFilename got %q, want %q", m.URL, m.OriginalFilename, w.file)
		}
	}

	// 重複リンク (abc123.png が2回) は1件、example.com は対象外。
	counts := map[string]int{}
	for _, m := range media {
		counts[m.URL]++
	}
	if counts["https://files.catbox.moe/abc123.png"] != 1 {
		t.Errorf("catbox 重複が1件に集約されていません: %v", counts)
	}
	for u := range counts {
		if strings.Contains(u, "example.com") {
			t.Errorf("メディア以外の URL を抽出しています: %s", u)
		}
	}
}

func TestFourChan_ReconstructHTML(t *testing.T) {
	t.Parallel()

	a := NewFourChanAdapter()
	raw := readJSONFixture(t, "fourchan_thread.json")
	content, err := a.ParseThreadHTML(raw)
	if err != nil {
		t.Fatalf("ParseThreadHTML: %v", err)
	}
	thread := model.ThreadInfo{ID: "1001", Title: "AI画像スレ & テスト"}
	media, err := a.ExtractMediaFiles(content, "https://a.4cdn.org/vg/thread/1001.json")
	if err != nil {
		t.Fatalf("ExtractMediaFiles: %v", err)
	}
	for i := range media {
		media[i].LocalPath = "./img/" + media[i].OriginalFilename
		if media[i].ThumbnailURL != "" {
			base := strings.TrimSuffix(media[i].OriginalFilename, filepath.Ext(media[i].OriginalFilename))
			media[i].LocalThumbPath = "./thumb/" + base + "s.jpg"
		}
	}

	out, err := a.ReconstructHTML(content, thread, media)
	if err != nil {
		t.Fatalf("ReconstructHTML: %v", err)
	}

	for _, want := range []string{
		"<title>AI画像スレ &amp; テスト</title>",
		"img/1700000000123.png",
		"thumb/1700000000123s.jpg",
		"img/abc123.png",
		"img/1700000000456.mp4",
		"img/def456.mp4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("生成 HTML に %q が含まれていません", want)
		}
	}

	// 外部 URL はローカルに置き換わる。
	for _, gone := range []string{
		"https://files.catbox.moe/abc123.png",
		"https://litter.catbox.moe/def456.mp4",
		"https://i.4cdn.org/vg/1700000000123.png",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("生成 HTML に外部 URL が残っています: %s", gone)
		}
	}

	// 引用リンク (quotelink) は作り直さない。
	if !strings.Contains(out, `class="quotelink"`) {
		t.Error("com の引用リンク (class=\"quotelink\") が保持されていません")
	}
	if !strings.Contains(out, `href="/vg/thread/1001#p1002"`) {
		t.Error("引用リンクの href が保持されていません")
	}

	// メディア以外の外部リンクは保持する。
	if !strings.Contains(out, "https://example.com/page.html") {
		t.Error("メディア以外の外部リンクが消えています")
	}
}

func TestFourChan_PrepareValidatesBoardHost(t *testing.T) {
	t.Parallel()

	a := NewFourChanAdapter()
	ok := []string{
		"https://boards.4chan.org/vg/",
		"https://boards.4channel.org/g/",
		"https://boards.4chan.org/vg",
	}
	for _, u := range ok {
		if err := a.Prepare(nil, config.Task{TargetBoardURL: u}); err != nil {
			t.Errorf("Prepare(%q) は成功すべきです: %v", u, err)
		}
	}

	ng := []string{
		"https://may.2chan.net/b/",
		"https://boards.4chan.org/",
		"https://example.com/vg/",
	}
	for _, u := range ng {
		if err := a.Prepare(nil, config.Task{TargetBoardURL: u}); err == nil {
			t.Errorf("Prepare(%q) は失敗すべきです", u)
		}
	}
}

// TestParseCatalogWithBoardDispatch は、任意インターフェースの分岐が
// 4chan では board 付き経路、ふたばでは従来経路になることを検証します。
func TestParseCatalogWithBoardDispatch(t *testing.T) {
	t.Parallel()

	// (1) 4chan: ParseCatalogWithBoard が呼ばれ、ThreadInfo.URL が絶対 URL になる。
	fourChan := NewFourChanAdapter()
	if _, ok := fourChan.(BoardScopedCatalogParser); !ok {
		t.Fatal("4chan アダプタは BoardScopedCatalogParser を実装すべきです")
	}
	threads, err := ParseCatalogWithBoard(fourChan, "https://a.4cdn.org/vg/catalog.json",
		readJSONFixture(t, "fourchan_catalog.json"))
	if err != nil {
		t.Fatalf("ParseCatalogWithBoard(4chan): %v", err)
	}
	for _, th := range threads {
		if !strings.HasPrefix(th.URL, "https://a.4cdn.org/vg/thread/") {
			t.Errorf("4chan のスレッド URL が絶対 URL ではありません: %q", th.URL)
		}
	}

	// (2) ふたば: 未実装のため従来の ParseCatalog にフォールバックする。
	futaba := NewFutabaAdapter()
	if _, ok := futaba.(BoardScopedCatalogParser); ok {
		t.Fatal("ふたばアダプタは BoardScopedCatalogParser を実装してはいけません (従来経路の維持)")
	}
	fixture := readFixture(t, phase2CatalogFixture)
	viaHelper, err := ParseCatalogWithBoard(futaba, "https://may.2chan.net/b/futaba.php?mode=cat", fixture)
	if err != nil {
		t.Fatalf("ParseCatalogWithBoard(futaba): %v", err)
	}
	direct, err := futaba.ParseCatalog(fixture)
	if err != nil {
		t.Fatalf("ParseCatalog(futaba): %v", err)
	}
	if len(viaHelper) != len(direct) || len(viaHelper) == 0 {
		t.Fatalf("ヘルパー経由と直接呼び出しの結果が異なります: %d vs %d", len(viaHelper), len(direct))
	}
	// Date は解析時刻 (time.Now) のため比較から除外し、それ以外の全フィールドを比較する。
	for i := range direct {
		if viaHelper[i].ID != direct[i].ID ||
			viaHelper[i].Title != direct[i].Title ||
			viaHelper[i].URL != direct[i].URL ||
			viaHelper[i].ResCount != direct[i].ResCount ||
			viaHelper[i].LastModified != direct[i].LastModified {
			t.Errorf("ふたばのフォールバック結果が直接呼び出しと一致しません [%d]: %+v vs %+v",
				i, viaHelper[i], direct[i])
		}
	}
	if strings.HasPrefix(viaHelper[0].URL, "https://") {
		t.Errorf("ふたばの URL は相対パスのままであるべきです: %q", viaHelper[0].URL)
	}
}
