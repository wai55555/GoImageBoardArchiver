package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"GoImageBoardArchiver/internal/adapter"
	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

var (
	futabaMediaPattern = regexp.MustCompile(`(\d{13,})(s?)\.(jpg|jpeg|png|webp|gif|webm|mp4|mp3|wav)`)

	catalogLinkPattern = regexp.MustCompile(`<a href="(res/(\d+)\.htm)"[^>]*>`)
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
	utf8Body, err := decodeShiftJIS(htmlBody)
	if err != nil {
		return nil, fmt.Errorf("文字コード変換に失敗しました: %w", err)
	}

	var threads []model.ThreadInfo
	matches := catalogLinkPattern.FindAllStringSubmatch(utf8Body, -1)
	seen := make(map[string]bool)

	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		href := m[1]
		id := m[2]

		if seen[id] {
			continue
		}
		seen[id] = true

		threads = append(threads, model.ThreadInfo{
			ID:       id,
			Title:    fmt.Sprintf("Thread %s", id),
			URL:      href,
			ResCount: 0,
			Date:     time.Now(),
		})
	}
	return threads, nil
}

// ParseThreadHTML は、スレッドページのHTMLをUTF-8文字列に変換します。
func (a *FutabaAdapter) ParseThreadHTML(htmlBody []byte) (string, error) {
	return decodeShiftJIS(htmlBody)
}

// ExtractMediaFiles は、スレッドのHTML文字列から正規表現にマッチするメディアファイル情報のみを抽出します。
func (a *FutabaAdapter) ExtractMediaFiles(htmlContent string, threadURL string) ([]model.MediaInfo, error) {
	base, err := url.Parse(threadURL)
	if err != nil {
		return nil, fmt.Errorf("スレッドURLの解析に失敗しました: %w", err)
	}

	hrefPattern := regexp.MustCompile(`href="([^"]+)"`)
	matches := hrefPattern.FindAllStringSubmatch(htmlContent, -1)

	var media []model.MediaInfo
	seen := make(map[string]bool)

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		rawHref := m[1]

		if !futabaMediaPattern.MatchString(filepath.Base(rawHref)) {
			continue
		}

		hrefURL, err := url.Parse(rawHref)
		if err != nil {
			continue
		}
		absURL := base.ResolveReference(hrefURL)
		absString := absURL.String()

		if seen[absString] {
			continue
		}
		seen[absString] = true

		media = append(media, model.MediaInfo{
			URL:              absString,
			OriginalFilename: filepath.Base(absURL.Path),
			ResNumber:        0,
		})
	}

	return media, nil
}

// ReconstructHTML は、HTML内のリンクをローカルパスに書き換え、クリーンアップします。
func (a *FutabaAdapter) ReconstructHTML(htmlContent string, thread model.ThreadInfo, mediaFiles []model.MediaInfo) (string, error) {
	htmlContent = regexp.MustCompile(`(?is)<script.*?>.*?</script>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<style.*?>.*?</style>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?i)<link\s+rel=["']?stylesheet["']?[^>]*>`).ReplaceAllString(htmlContent, "")

	for _, mf := range mediaFiles {
		filename := filepath.Base(mf.URL)
		localFilename := filepath.Base(mf.LocalPath)
		if localFilename == "" {
			localFilename = filename
		}

		targetPath := filepath.ToSlash(filepath.Join("img", localFilename))
		htmlContent = strings.ReplaceAll(htmlContent, mf.URL, targetPath)

		relPath := "src/" + filename
		htmlContent = strings.ReplaceAll(htmlContent, relPath, targetPath)

		thumbFilename := strings.Replace(filename, ".", "s.", 1)
		thumbLocal := filepath.ToSlash(filepath.Join("thumb", thumbFilename))
		htmlContent = strings.ReplaceAll(htmlContent, "thumb/"+thumbFilename, thumbLocal)
	}

	htmlContent = regexp.MustCompile(`(?i)<meta\s+http-equiv=["']?Content-Type["']?[^>]*>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?i)<meta\s+charset=["']?[^"'>]+["']?>`).ReplaceAllString(htmlContent, "")

	if strings.Contains(htmlContent, "<head>") {
		newHead := `<head>
<meta charset="UTF-8">
<link rel="stylesheet" href="css/futaba.css">`
		htmlContent = strings.Replace(htmlContent, "<head>", newHead, 1)
	}

	return htmlContent, nil
}

func decodeShiftJIS(b []byte) (string, error) {
	reader := transform.NewReader(bytes.NewReader(b), japanese.ShiftJIS.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
