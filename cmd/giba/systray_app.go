package main

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

	"GoImageBoardArchiver/internal/adapter"
	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

var (
	futabaMediaPattern = regexp.MustCompile(`(\d{13,})(s?)\.(jpg|jpeg|png|webp|gif|webm|mp4|mp3|wav)`)
	threadIDRegex      = regexp.MustCompile(`res/(\d+)\.htm`)
)

// FutabaAdapter は、ふたば☆ちゃんねる固有の解析ロジックを実装します。
type FutabaAdapter struct{}

// NewFutabaAdapter は、FutabaAdapterの新しいインスタンスを返します。
func NewFutabaAdapter() adapter.SiteAdapter {
	return &FutabaAdapter{}
}

// Prepare は、ふたばちゃんねる用の準備として'cxyl' Cookieを設定します。
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

// BuildCatalogURL は、ふたばちゃんねるのカタログURLを構築します。
func (a *FutabaAdapter) BuildCatalogURL(baseURL string) (string, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("ベースURLの解析に失敗しました: %w", err)
	}
	parsedURL.Path = path.Join(parsedURL.Path, "futaba.php")
	query := url.Values{}
	query.Set("mode", "cat")
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

// ParseCatalog は、ふたばちゃんねるのカタログページのHTMLコンテンツを解析します。
func (a *FutabaAdapter) ParseCatalog(htmlBody []byte) ([]model.ThreadInfo, error) {
	utf8Reader := transform.NewReader(bytes.NewReader(htmlBody), japanese.ShiftJIS.NewDecoder())
	doc, err := goquery.NewDocumentFromReader(utf8Reader)
	if err != nil {
		return nil, fmt.Errorf("カタログHTMLの解析に失敗しました: %w", err)
	}

	var threads []model.ThreadInfo
	doc.Find("td > a[href*='res/']").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		threadID := extractThreadID(href)
		if threadID == "" {
			return
		}

		title := s.Find("small").Text()

		threads = append(threads, model.ThreadInfo{
			ID:    threadID,
			Title: title,
			URL:   href,
		})
	})
	return threads, nil
}

// ParseThreadHTML は、スレッドページのHTMLをgoquery.Documentに変換します。
func (a *FutabaAdapter) ParseThreadHTML(htmlBody []byte) (*goquery.Document, error) {
	utf8Reader := transform.NewReader(bytes.NewReader(htmlBody), japanese.ShiftJIS.NewDecoder())
	doc, err := goquery.NewDocumentFromReader(utf8Reader)
	if err != nil {
		return nil, fmt.Errorf("スレッドHTMLの解析に失敗しました: %w", err)
	}
	return doc, nil
}

// ExtractMediaFiles は、スレッドのDOMから正規表現にマッチするメディアファイル情報のみを抽出します。
func (a *FutabaAdapter) ExtractMediaFiles(doc *goquery.Document, threadURL string) ([]model.MediaInfo, error) {
	baseParsedURL, err := url.Parse(threadURL)
	if err != nil {
		return nil, fmt.Errorf("スレッドURLの解析に失敗しました: %w", err)
	}

	var mediaFiles []model.MediaInfo
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		if !futabaMediaPattern.MatchString(href) {
			return
		}

		absoluteURL := baseParsedURL.ResolveReference(&url.URL{Path: href})

		var resNum int
		resNumStr := s.Closest("td").Find("input[type=checkbox]").AttrOr("name", "0")
		resNum, _ = strconv.Atoi(resNumStr)

		mediaFiles = append(mediaFiles, model.MediaInfo{
			URL:              absoluteURL.String(),
			OriginalFilename: filepath.Base(absoluteURL.Path),
			ResNumber:        resNum,
		})
	})
	return mediaFiles, nil
}

// ReconstructHTML は、HTML内のリンクをローカルパスに書き換え、クリーンアップします。
func (a *FutabaAdapter) ReconstructHTML(doc *goquery.Document, thread model.ThreadInfo, mediaFiles []model.MediaInfo) (string, error) {
	urlToLocalFilename := make(map[string]string)
	for _, mf := range mediaFiles {
		if mf.LocalPath != "" {
			urlToLocalFilename[mf.URL] = filepath.Base(mf.LocalPath)
		}
	}

	doc.Find("script, style, link[rel='stylesheet']").Remove()

	base, err := url.Parse(thread.URL)
	if err != nil {
		return "", fmt.Errorf("スレッドURL '%s' の解析に失敗しました: %w", thread.URL, err)
	}

	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		originalHref, exists := s.Attr("href")
		if !exists {
			return
		}

		resolvedURL := base.ResolveReference(&url.URL{Path: originalHref})

		if localFilename, ok := urlToLocalFilename[resolvedURL.String()]; ok {
			newPath := path.Join("media", localFilename)
			s.SetAttr("href", newPath)
			s.Find("img").SetAttr("src", newPath)
		}
	})

	return doc.Html()
}

func extractThreadID(href string) string {
	matches := threadIDRegex.FindStringSubmatch(href)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}
