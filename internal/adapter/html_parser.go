package adapter

import (
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"GoImageBoardArchiver/internal/model"

	"github.com/PuerkitoBio/goquery"
)

// futabaMediaPattern is reused from futaba_adapter.go for consistency.
var futabaMediaPatternParser = regexp.MustCompile(`(\d{13,})(s?)\.(jpg|jpeg|png|webp|gif|webm|mp4|mp3|wav)`)
var futabaThreadIDPattern = regexp.MustCompile(`res/(\d+)\.htm`)

// ParseCatalogWithGoQuery uses goquery to parse the catalog HTML.
// It is designed to be more robust against HTML structure changes than the regex version.
func ParseCatalogWithGoQuery(htmlBody []byte) ([]model.ThreadInfo, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBody))
	if err != nil {
		return nil, fmt.Errorf("goquery: ドキュメントの読み込みに失敗しました: %w", err)
	}

	var threads []model.ThreadInfo
	seen := make(map[string]bool)

	// カタログのスレッドは 'td' タグ内にあることが多い
	doc.Find("td").Each(func(i int, s *goquery.Selection) {
		linkTag := s.Find("a[href*='res/']")
		href, exists := linkTag.Attr("href")
		if !exists {
			return
		}

		// スレッドIDを抽出
		matches := futabaThreadIDPattern.FindStringSubmatch(href)
		if len(matches) < 2 {
			return
		}
		id := matches[1]

		if seen[id] {
			return
		}
		seen[id] = true

		// タイトルを抽出 (<small>タグ)
		title := s.Find("small").Text()
		title = strings.TrimSpace(title)
		if title == "" {
			title = fmt.Sprintf("Thread %s", id) // fallback
		}

		threads = append(threads, model.ThreadInfo{
			ID:       id,
			Title:    title,
			URL:      href, // URLは相対パスのまま
			ResCount: 0,    // カタログからは取得しない
			Date:     time.Now(), // カタログからは取得しない
		})
	})

	return threads, nil
}

// ExtractMediaFilesWithGoQuery uses goquery to extract media links from a thread's HTML.
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

	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")

		// ファイル名がふたばのメディア形式かチェック
		if !futabaMediaPatternParser.MatchString(filepath.Base(href)) {
			return
		}

		// 絶対URLに変換
		hrefURL, err := url.Parse(href)
		if err != nil {
			return
		}
		absURL := base.ResolveReference(hrefURL)
		absString := absURL.String()

		if seen[absString] {
			return
		}
		seen[absString] = true

		// サムネイルURLの推測
		thumbnailURL := ""
		imgTag := s.Find("img")
		if src, exists := imgTag.Attr("src"); exists {
			thumbAbsURL := base.ResolveReference(&url.URL{Path: src})
			thumbnailURL = thumbAbsURL.String()
		}

		media = append(media, model.MediaInfo{
			URL:              absString,
			OriginalFilename: filepath.Base(absURL.Path),
			ThumbnailURL:     thumbnailURL,
			ResNumber:        0, // goqueryでもレス番号の特定は複雑なため、別途対応
		})
	})

	return media, nil
}
