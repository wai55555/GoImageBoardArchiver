package adapter

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"GoImageBoardArchiver/internal/model"

	"github.com/PuerkitoBio/goquery"
)

// ふたばのメディアファイル名パターン (13桁以上の数字 + 任意の 's' + 拡張子)。
// 抽出結果を正規表現版と一致させるため、メディア判定だけは引き続きこのパターンを使う。
var futabaMediaPatternParser = regexp.MustCompile(`(\d{13,})(s?)\.(jpg|jpeg|png|webp|gif|webm|mp4|mp3|wav)`)

// スレッドID抽出用 (res/123456789.htm)。
var futabaThreadIDPattern = regexp.MustCompile(`res/(\d+)\.htm`)

// タイトルからタグを除去するためのパターン。
var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// ParseCatalogWithGoQuery は、カタログHTML (Shift_JIS) を goquery で解析して
// スレッド情報を返します。
//
// 正規表現版 (旧 futaba_adapter.go) と同一の結果になるよう、次の仕様を踏襲しています:
//   - href に `res/<数字>.htm` を含むリンクを文書順に走査し、ID の重複は最初の1件のみ採用
//   - タイトルは、リンクを含むセル内の最初の <small> の内部 HTML を使い、
//     <br> を空白に、それ以外のタグを除去する (HTML エンティティはデコードしない)
//   - <small> が空なら "Thread <ID>"、URL は href の生値 (相対パスのまま)
//   - ResCount は 0、Date は解析時刻
func ParseCatalogWithGoQuery(htmlBody []byte) ([]model.ThreadInfo, error) {
	utf8Body, err := decodeShiftJIS(htmlBody)
	if err != nil {
		return nil, fmt.Errorf("文字コード変換に失敗しました: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(utf8Body))
	if err != nil {
		return nil, fmt.Errorf("goquery: ドキュメントの読み込みに失敗しました: %w", err)
	}

	var threads []model.ThreadInfo
	seen := make(map[string]bool)

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}

		matches := futabaThreadIDPattern.FindStringSubmatch(href)
		if len(matches) < 2 {
			return
		}
		id := matches[1]
		if seen[id] {
			return
		}
		seen[id] = true

		title := fmt.Sprintf("Thread %s", id)
		if extracted := catalogThreadTitle(s); extracted != "" {
			title = extracted
		}

		threads = append(threads, model.ThreadInfo{
			ID:       id,
			Title:    title,
			URL:      href, // 正規表現版と同じく相対パスのまま
			ResCount: 0,
			Date:     time.Now(),
		})
	})

	return threads, nil
}

// catalogThreadTitle は、スレッドリンクを含むセル内の最初の <small> から
// 正規表現版と同じ手順でタイトル文字列を取り出します。
func catalogThreadTitle(link *goquery.Selection) string {
	scope := link.Closest("td")
	if scope.Length() == 0 {
		scope = link.Parent()
	}

	small := scope.Find("small").First()
	if small.Length() == 0 {
		return ""
	}

	inner, err := small.Html()
	if err != nil {
		return ""
	}

	// 正規表現版は "<br>" を空白に置換してからタグを除去していた。
	// goquery (net/html) は <br> を <br/> として再描画するため、両方を受ける。
	extracted := strings.ReplaceAll(inner, "<br>", " ")
	extracted = strings.ReplaceAll(extracted, "<br/>", " ")
	extracted = htmlTagPattern.ReplaceAllString(extracted, "")
	return extracted
}

// ExtractMediaFilesWithGoQuery は、スレッドHTMLから goquery でメディアリンクを抽出します。
//
// 正規表現版 (旧 futaba_adapter.go) と同一の結果になるよう、次の仕様を踏襲しています:
//   - href 属性を持つ要素を文書順に走査し、ファイル名がふたばのメディア形式のものだけ採用
//   - 絶対URLに解決し、URL の重複は最初の1件のみ採用
//   - サムネイルは href のパスから /src/ → /thumb/、ファイル名を <name>s.jpg に変換して導出
//   - ResNumber は 0
//
// 正規表現版との意図的な差分: 正規表現版は引用符なしの href が後続リンクの
// 開始引用符を飲み込んでしまい、その次のメディアを取りこぼしていた。
// goquery 版は HTML の属性として正しく解釈するため取りこぼさない。
func ExtractMediaFilesWithGoQuery(htmlContent string, threadURL string) ([]model.MediaInfo, error) {
	base, err := url.Parse(threadURL)
	if err != nil {
		return nil, fmt.Errorf("スレッドURLの解析に失敗しました: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("goquery: ドキュメントの読み込みに失敗しました: %w", err)
	}

	var media []model.MediaInfo
	seen := make(map[string]bool)

	doc.Find("[href]").Each(func(_ int, s *goquery.Selection) {
		rawHref, ok := s.Attr("href")
		if !ok {
			return
		}

		if !futabaMediaPatternParser.MatchString(filepath.Base(rawHref)) {
			return
		}

		hrefURL, err := url.Parse(rawHref)
		if err != nil {
			return
		}
		absURL := base.ResolveReference(hrefURL)
		absString := absURL.String()
		if seen[absString] {
			return
		}
		seen[absString] = true

		originalFilename := filepath.Base(absURL.Path)
		ext := filepath.Ext(originalFilename)
		thumbFilename := strings.TrimSuffix(originalFilename, ext) + "s.jpg"

		thumbPath := strings.Replace(absURL.Path, "/src/", "/thumb/", 1)
		thumbPath = strings.Replace(thumbPath, originalFilename, thumbFilename, 1)
		thumbnailURL := ""
		if thumbURL, err := url.Parse(thumbPath); err == nil {
			thumbnailURL = base.ResolveReference(thumbURL).String()
		}

		media = append(media, model.MediaInfo{
			URL:              absString,
			OriginalFilename: originalFilename,
			ThumbnailURL:     thumbnailURL,
			ResNumber:        0,
		})
	})

	return media, nil
}
