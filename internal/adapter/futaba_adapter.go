package adapter

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

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

var (
	// ふたばちゃんねるの正規メディアファイル名を検出 (13桁以上の数字 + 任意の 's' + 拡張子)
	futabaMediaPattern = regexp.MustCompile(`(\d{13,})(s?)\.(jpg|jpeg|png|webp|gif|webm|mp4|mp3|wav)`)
	// スレッドID抽出用 (res/123456789.htm)

	// カタログからのスレッド情報抽出用 (簡易的な正規表現)
	// href属性内に res/<数字>.htm が含まれるものを抽出。シングル/ダブルクォート、前置きの ./ や パスも許容
	catalogLinkPattern = regexp.MustCompile(`href=["']?([^"'>]*?res/(\d+)\.htm)["']?`)
)

// FutabaAdapter は、ふたば☆ちゃんねる固有の解析ロジックを実装します。
type FutabaAdapter struct{}

// NewFutabaAdapter は、FutabaAdapterの新しいインスタンスを返します。
func NewFutabaAdapter() SiteAdapter {
	return &FutabaAdapter{}
}

// Prepare は、ふたばちゃんねる用の準備として 'cxyl' Cookie を設定します。
func (a *FutabaAdapter) Prepare(client *network.Client, taskConfig config.Task) error {
	// FutabaCatalogSettingsが設定されていない場合はデフォルト値を使用
	if taskConfig.FutabaCatalogSettings == nil {
		log.Println("INFO: FutabaCatalogSettingsが設定されていないため、デフォルト値(9x100x20)を使用します")
		taskConfig.FutabaCatalogSettings = &config.FutabaCatalogSettings{
			Cols:        9,
			Rows:        100,
			TitleLength: 20,
		}
	}

	// 各値が0の場合もデフォルト値を使用
	cols := taskConfig.FutabaCatalogSettings.Cols
	if cols <= 0 {
		cols = 9
	}
	rows := taskConfig.FutabaCatalogSettings.Rows
	if rows <= 0 {
		rows = 100
	}
	titleLength := taskConfig.FutabaCatalogSettings.TitleLength
	if titleLength <= 0 {
		titleLength = 20
	}

	cookieValue := fmt.Sprintf("%dx%dx%dx0x0", cols, rows, titleLength)
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
// 正規表現を用いてリンクと、その周辺のテキスト（タイトルとして使用）を抽出します。
func (a *FutabaAdapter) ParseCatalog(htmlBody []byte) ([]model.ThreadInfo, error) {
	// Shift_JIS -> UTF-8 変換
	utf8BodyStr, err := decodeShiftJIS(htmlBody)
	if err != nil {
		return nil, fmt.Errorf("文字コード変換に失敗しました: %w", err)
	}

	var threads []model.ThreadInfo
	// href="res/..." を持つ箇所をスレッドリンクとみなす
	// FindAllStringSubmatchIndex を使用して位置を取得する
	matches := catalogLinkPattern.FindAllStringSubmatchIndex(utf8BodyStr, -1)
	seen := make(map[string]bool)

	// タイトル抽出用の正規表現 (<small>...</small> または title属性)
	// ふたばのカタログ(mode=cat)は通常、リンクの後に <small>本文</small> が続く
	smallTagPattern := regexp.MustCompile(`<small>(.*?)</small>`)

	for _, m := range matches {
		if len(m) < 6 {
			continue
		}
		// m[2]:m[3] -> href (URL)
		// m[4]:m[5] -> ID
		href := utf8BodyStr[m[2]:m[3]]
		id := utf8BodyStr[m[4]:m[5]]

		if seen[id] {
			continue
		}
		seen[id] = true

		// タイトル抽出: リンクの後ろ300文字程度を検索
		endPos := m[1]
		searchLimit := endPos + 300
		if searchLimit > len(utf8BodyStr) {
			searchLimit = len(utf8BodyStr)
		}
		searchArea := utf8BodyStr[endPos:searchLimit]

		title := fmt.Sprintf("Thread %s", id) // デフォルト
		if match := smallTagPattern.FindStringSubmatch(searchArea); len(match) > 1 {
			// タグ除去などのクリーニングが必要ならここで行う
			extracted := match[1]
			// <br>などをスペースに置換
			extracted = strings.ReplaceAll(extracted, "<br>", " ")
			// HTMLタグを除去 (簡易)
			extracted = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(extracted, "")
			if extracted != "" {
				title = extracted
			}
		}

		threads = append(threads, model.ThreadInfo{
			ID:       id,
			Title:    title,
			URL:      href,
			ResCount: 0,
			Date:     time.Now(),
		})
	}

	return threads, nil
}

// ParseThreadHTML は、スレッドHTMLを Shift_JIS -> UTF-8 に変換して文字列として返します。
func (a *FutabaAdapter) ParseThreadHTML(htmlBody []byte) (string, error) {
	return decodeShiftJIS(htmlBody)
}

// ExtractMediaFiles は、スレッドHTML文字列から正規表現を用いてメディアリンクを抽出します。
func (a *FutabaAdapter) ExtractMediaFiles(htmlContent string, threadURL string) ([]model.MediaInfo, error) {
	base, err := url.Parse(threadURL)
	if err != nil {
		return nil, fmt.Errorf("スレッドURLの解析に失敗しました: %w", err)
	}

	// <a ... href="src/123456789.jpg" ...> のようなパターンを探す
	// 引用符はシングル/ダブル両対応
	hrefPattern := regexp.MustCompile(`href=["']?([^"']+)["']?`)
	matches := hrefPattern.FindAllStringSubmatch(htmlContent, -1)

	var media []model.MediaInfo
	seen := make(map[string]bool)

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		rawHref := m[1]

		// ファイル名がふたばのメディア形式かチェック
		if !futabaMediaPattern.MatchString(filepath.Base(rawHref)) {
			continue
		}

		// 絶対URLに変換
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

		// サムネイルURLの推測
		// ふたばの標準: src/1234567890.jpg -> thumb/1234567890s.jpg
		originalFilename := filepath.Base(absURL.Path)
		thumbnailURL := ""

		// ファイル名から拡張子を分離
		ext := filepath.Ext(originalFilename)
		nameWithoutExt := strings.TrimSuffix(originalFilename, ext)

		// サムネイル用のファイル名を生成 (例: 1234567890 -> 1234567890s)
		// ふたばのサムネイルは常にjpgなので拡張子を.jpgに固定
		thumbFilename := nameWithoutExt + "s.jpg"

		// サムネイルのURLを構築
		thumbPath := strings.Replace(absURL.Path, "/src/", "/thumb/", 1)
		thumbPath = strings.Replace(thumbPath, originalFilename, thumbFilename, 1)
		thumbURL, _ := url.Parse(thumbPath)
		if thumbURL != nil {
			thumbnailURL = base.ResolveReference(thumbURL).String()
		}

		media = append(media, model.MediaInfo{
			URL:              absString,
			OriginalFilename: originalFilename,
			ThumbnailURL:     thumbnailURL,
			// ResNumber: レス番号の抽出は正規表現だと困難なため、0とするか別途解析が必要
			ResNumber: 0,
		})
	}

	return media, nil
}

// ReconstructHTML は、収集済みメディアのURL→ローカルファイル名のマッピングに基づいてリンクを書き換えます。
// 文字列置換を使用します。
func (a *FutabaAdapter) ReconstructHTML(htmlContent string, thread model.ThreadInfo, mediaFiles []model.MediaInfo) (string, error) {
	// 1. 不要なタグの削除 (script, style, link)
	// 正規表現で簡易的に削除
	htmlContent = regexp.MustCompile(`(?is)<script.*?>.*?</script>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?is)<style.*?>.*?</style>`).ReplaceAllString(htmlContent, "")
	htmlContent = regexp.MustCompile(`(?i)<link\s+rel=["']?stylesheet["']?[^>]*>`).ReplaceAllString(htmlContent, "")

	// 2. リンクの書き換え
	// 単純な文字列置換を行う。URLの一部が他のURLに含まれる場合のリスクはあるが、
	// ふたばのファイル名はユニーク性が高いため衝突しにくい。
	for _, mf := range mediaFiles {
		filename := filepath.Base(mf.URL)

		// LocalPathが設定されていない場合のfallback: 元のファイル名を使用
		localFilename := filepath.Base(mf.LocalPath)
		if localFilename == "" || localFilename == "." {
			localFilename = filename
			log.Printf("WARNING: LocalPathが設定されていないため、元のファイル名を使用します: %s", filename)
		}

		// フルサイズ画像へのリンク (href=".../123.jpg") -> href="img/123.jpg"
		// 注意: 単純置換だと誤爆の可能性があるため、ファイル名単位で置換する
		// ただし、URL全体で置換するのが最も安全
		targetPath := filepath.ToSlash(filepath.Join("img", localFilename))

		// 完全なURLを置換 (https://may.2chan.net/b/src/123.jpg)
		htmlContent = strings.ReplaceAll(htmlContent, mf.URL, targetPath)

		// 絶対パスを置換 (/b/src/123.jpg)
		absPath := "/b/src/" + filename
		htmlContent = strings.ReplaceAll(htmlContent, absPath, targetPath)

		// 相対パスを置換 (src/123.jpg)
		relPath := "src/" + filename
		htmlContent = strings.ReplaceAll(htmlContent, relPath, targetPath)

		// サムネイル (thumb/...) -> thumb/localFilename
		// LocalThumbPathが設定されている場合はそれを使用、なければ推測
		var thumbLocalFilename string
		if mf.LocalThumbPath != "" {
			thumbLocalFilename = filepath.Base(mf.LocalThumbPath)
		} else {
			// 推測: 123.jpg -> 123s.jpg
			// ふたばのサムネイルは常にjpgなので拡張子を.jpgに固定
			ext := filepath.Ext(filename)
			nameWithoutExt := strings.TrimSuffix(filename, ext)
			thumbLocalFilename = nameWithoutExt + "s.jpg"
		}

		thumbLocal := filepath.ToSlash(filepath.Join("thumb", thumbLocalFilename))

		// サムネイルの元のパターンを置換
		// ふたばのサムネイルは常にjpgなので拡張子を.jpgに固定
		ext := filepath.Ext(filename)
		nameWithoutExt := strings.TrimSuffix(filename, ext)
		thumbFilename := nameWithoutExt + "s.jpg"

		// ThumbnailURLが設定されている場合は、完全なURLを置換
		if mf.ThumbnailURL != "" {
			htmlContent = strings.ReplaceAll(htmlContent, mf.ThumbnailURL, thumbLocal)
		}

		// 絶対パスを置換 (/b/thumb/123s.jpg)
		absThumbPath := "/b/thumb/" + thumbFilename
		htmlContent = strings.ReplaceAll(htmlContent, absThumbPath, thumbLocal)

		// 相対パスを置換 (thumb/123s.jpg)
		relThumbPath := "thumb/" + thumbFilename
		htmlContent = strings.ReplaceAll(htmlContent, relThumbPath, thumbLocal)
	}

	// 3. ヘッダーの調整
	// meta charsetなどをUTF-8に
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
