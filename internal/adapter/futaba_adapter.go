package adapter

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

var (
	// ふたばちゃんねるの正規メディアファイル名を検出 (13桁以上の数字 + 任意の 's' + 拡張子)
	futabaMediaPattern = regexp.MustCompile(`(\d{13,})(s?)\.(jpg|jpeg|png|webp|gif|webm|mp4|mp3|wav)`)
	// スレッドID抽出用 (res/123456789.htm)
	threadIDRegex = regexp.MustCompile(`res/(\d+)\.htm`)
)

// FutabaAdapter は、ふたば☆ちゃんねる固有の解析ロジックを実装します。
type FutabaAdapter struct{}

// NewFutabaAdapter は、FutabaAdapterの新しいインスタンスを返します。
func NewFutabaAdapter() SiteAdapter {
	return &FutabaAdapter{}
}

// Prepare は、ふたばちゃんねる用の準備として 'cxyl' Cookie を設定します。
func (a *FutabaAdapter) Prepare(client *network.Client, taskConfig config.Task) error {
	if taskConfig.FutabaCatalogSettings == nil {
		return nil
	}
	cookieValue := fmt.Sprintf("%dx%dx%dx0x0",
		taskConfig.FutabaCatalogSettings.Cols,
		taskConfig.FutabaCatalogSettings.Rows,
		taskConfig.FutabaCatalogSettings.TitleLength,
	)
	cookie := &http.Cookie{
		Name:   "cxyl",
		Value:  cookieValue,
		Path:   "/",
		Domain: ".2chan.net",
	}
	log.Println("DEBUG: futaba_adapterが生成したCookieを設定します:", cookie)
	return client.SetCookie(taskConfig.TargetBoardURL, cookie)
}

// BuildCatalogURL は、ふたばのカタログURLを構築します。
func (a *FutabaAdapter) BuildCatalogURL(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("ベースURLの解析に失敗しました: %w", err)
	}
	u.Path = path.Join(u.Path, "futaba.php")
	q := url.Values{}
	q.Set("mode", "cat")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ParseCatalog は、カタログHTMLを解析し、スレッド情報のスライスを返します。
func (a *FutabaAdapter) ParseCatalog(htmlBody []byte) ([]model.ThreadInfo, error) {
	utf8Reader := transform.NewReader(bytes.NewReader(htmlBody), japanese.ShiftJIS.NewDecoder())
	doc, err := goquery.NewDocumentFromReader(utf8Reader)
	if err != nil {
		return nil, fmt.Errorf("カタログHTMLの解析に失敗しました: %w", err)
	}

	threads := make([]model.ThreadInfo, 0, 256)
	// td > a[href*='res/'] を起点に抽出
	doc.Find("td > a[href*='res/']").Each(func(i int, link *goquery.Selection) {
		href, ok := link.Attr("href")
		if !ok || href == "" {
			return
		}
		threadID := extractThreadID(href)
		if threadID == "" {
			return
		}

		// タイトルは同じ td 内の small を優先
		td := link.Parent()
		title := strings.TrimSpace(td.Find("small").First().Text())
		if title == "" {
			title = "タイトルなし"
		}

		// レス数 (存在しない/非数なら0)
		resText := strings.TrimSpace(td.Find(`font[size="2"]`).First().Text())
		resCount, _ := strconv.Atoi(resText)

		threads = append(threads, model.ThreadInfo{
			ID:       threadID,
			Title:    title,
			URL:      href,
			ResCount: resCount,
			Date:     time.Now(),
		})
	})

	return threads, nil
}

// ParseThreadHTML は、スレッドHTMLを Shift_JIS -> UTF-8 に変換して goquery.Document を返します。
func (a *FutabaAdapter) ParseThreadHTML(htmlBody []byte) (*goquery.Document, error) {
	utf8Reader := transform.NewReader(bytes.NewReader(htmlBody), japanese.ShiftJIS.NewDecoder())
	doc, err := goquery.NewDocumentFromReader(utf8Reader)
	if err != nil {
		return nil, fmt.Errorf("スレッドHTMLの解析に失敗しました: %w", err)
	}
	return doc, nil
}

// ExtractMediaFiles は、スレッドDOM内の <a> を全探索し、正規のメディアリンクのみを抽出します。
func (a *FutabaAdapter) ExtractMediaFiles(doc *goquery.Document, threadURL string) ([]model.MediaInfo, error) {
	base, err := url.Parse(threadURL)
	if err != nil {
		return nil, fmt.Errorf("スレッドURLの解析に失敗しました: %w", err)
	}

	media := make([]model.MediaInfo, 0, 256)
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		if !futabaMediaPattern.MatchString(filepath.Base(href)) {
			return
		}

		hrefURL, parseErr := url.Parse(href)
		if parseErr != nil {
			return
		}
		abs := base.ResolveReference(hrefURL)

		// サムネイルを探す
		thumbSrc := s.Find("img").AttrOr("src", "")
		var thumbAbs string
		if thumbSrc != "" {
			if thumbURL, err := url.Parse(thumbSrc); err == nil {
				thumbAbs = base.ResolveReference(thumbURL).String()
			}
		}

		resName := s.Closest("td").Find("input[type=checkbox]").First().AttrOr("name", "0")
		resNum, _ := strconv.Atoi(resName)

		media = append(media, model.MediaInfo{
			URL:              abs.String(),
			ThumbnailURL:     thumbAbs,
			OriginalFilename: filepath.Base(abs.Path),
			ResNumber:        resNum,
		})
	})

	return media, nil
}

// ReconstructHTML は、収集済みメディアのURL→ローカルファイル名のマッピングに基づいてリンクを書き換えます。
func (a *FutabaAdapter) ReconstructHTML(doc *goquery.Document, thread model.ThreadInfo, mediaFiles []model.MediaInfo) (string, error) {
	// 不要なタグを削除（スクリプト、スタイル、外部スタイルシート）
	doc.Find("script, style, link[rel='stylesheet']").Remove()

	// a[href] のローカル化（リンク先はフルサイズのローカルファイルへ）
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		baseHref := filepath.Base(href)

		for _, mf := range mediaFiles {
			fullBase := filepath.Base(mf.URL)
			thumbBase := filepath.Base(mf.ThumbnailURL)

			// ---- フルサイズ画像の置き換え ----
			if baseHref == fullBase {
				target := filepath.Base(mf.LocalPath)
				if target == "" {
					// fallback: LocalPath が空なら元の basename を使う
					target = fullBase
				}
				// フルサイズ画像は img/ に置換
				s.SetAttr("href", filepath.ToSlash(filepath.Join("img", target)))
			}

			// ---- サムネイル画像の置き換え ----
			s.Find("img[src]").Each(func(_ int, img *goquery.Selection) {
				src, _ := img.Attr("src")
				srcBase := filepath.Base(src)
				if srcBase == thumbBase {
					thumbTarget := filepath.Base(mf.LocalThumbPath)
					if thumbTarget == "" {
						// fallback: LocalThumbPath が空なら元の basename を使う
						thumbTarget = thumbBase
					}
					// サムネイルは thumb/ に置換
					img.SetAttr("src", filepath.ToSlash(filepath.Join("thumb", thumbTarget)))
				}
			})
		}
	})

	// HTMLの文字コード宣言をUTF-8に統一（Shift_JISのままだと文字化け）
	doc.Find("meta[http-equiv='Content-Type']").Remove()
	doc.Find("meta[charset]").Remove()
	head := doc.Find("head")
	if head.Length() > 0 {
		// CSSリンクを追加（css/futaba.css を参照）
		head.PrependHtml(`<link rel="stylesheet" href="css/futaba.css">`)
		head.PrependHtml(`<meta charset="UTF-8">`)
	}

	return doc.Html()
}

func extractThreadID(href string) string {
	m := threadIDRegex.FindStringSubmatch(href)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
