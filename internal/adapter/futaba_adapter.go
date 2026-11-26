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

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
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
// 解析は goquery 版 (html_parser.go) に委譲します。
func (a *FutabaAdapter) ParseCatalog(htmlBody []byte) ([]model.ThreadInfo, error) {
	return ParseCatalogWithGoQuery(htmlBody)
}

// ParseThreadHTML は、スレッドHTMLを Shift_JIS -> UTF-8 に変換して文字列として返します。
func (a *FutabaAdapter) ParseThreadHTML(htmlBody []byte) (string, error) {
	return decodeShiftJIS(htmlBody)
}

// ExtractMediaFiles は、スレッドHTML文字列からメディアリンクを抽出します。
// 解析は goquery 版 (html_parser.go) に委譲します。
func (a *FutabaAdapter) ExtractMediaFiles(htmlContent string, threadURL string) ([]model.MediaInfo, error) {
	return ExtractMediaFilesWithGoQuery(htmlContent, threadURL)
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
