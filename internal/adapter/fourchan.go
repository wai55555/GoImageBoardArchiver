package adapter

// 4chan アダプタ (オプション Phase 6 / OPT-1)。
//
// 4chan は HTML をスクレイピングせず、公式 JSON API を使う。
//   catalog: https://a.4cdn.org/<board>/catalog.json  (ルートは {page, threads} の配列)
//   thread : https://a.4cdn.org/<board>/thread/<no>.json
//   画像   : https://i.4cdn.org/<board>/<tim><ext> / <tim>s.jpg
//
// board はアダプタのフィールドに保持しない (監視/手動実行の並行で上書きされ得るため)。
// ParseCatalogWithBoard は呼び出しごとに catalogURL から board を導出する。

import (
	"encoding/json"
	"fmt"
	"html"
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
)

const (
	fourChanAPIBase = "https://a.4cdn.org"
	fourChanCDNBase = "https://i.4cdn.org"
)

// FourChanAdapter は 4chan の SiteAdapter 実装です。
type FourChanAdapter struct{}

// NewFourChanAdapter は新しい FourChanAdapter を返します。
func NewFourChanAdapter() SiteAdapter {
	return &FourChanAdapter{}
}

var (
	fourChanBoardPattern = regexp.MustCompile(`^[a-z0-9]{1,10}$`)
	fourChanURLPattern   = regexp.MustCompile(`https?://[^\s"'<>]+`)
)

// catboxHosts は 4chan の AI 画像スレッドで使われる外部メディアホストです。
// 4chan 側のメタデータが消えるため、本文中の catbox リンクも保存対象にする。
var catboxHosts = map[string]bool{
	"files.catbox.moe":     true,
	"litter.catbox.moe":    true,
	"litterbox.catbox.moe": true,
}

// externalMediaExts は catbox から取得してよいメディア拡張子です。
var externalMediaExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".mp4": true, ".webm": true, ".mp3": true, ".wav": true,
}

type fourChanCatalogPage struct {
	Page    int                     `json:"page"`
	Threads []fourChanCatalogThread `json:"threads"`
}

type fourChanCatalogThread struct {
	No           int64  `json:"no"`
	Sub          string `json:"sub"`
	Com          string `json:"com"`
	Images       int    `json:"images"`
	Replies      int    `json:"replies"`
	Tim          int64  `json:"tim"`
	Ext          string `json:"ext"`
	LastModified int64  `json:"last_modified"`
}

type fourChanThread struct {
	Posts []fourChanPost `json:"posts"`
}

type fourChanPost struct {
	No       int    `json:"no"`
	Name     string `json:"name"`
	Time     int64  `json:"time"`
	Now      string `json:"now"`
	Sub      string `json:"sub"`
	Com      string `json:"com"`
	Tim      int64  `json:"tim"`
	Ext      string `json:"ext"`
	Filename string `json:"filename"`
	ID       string `json:"id"`
}

// fourChanBoardFromPath は URL のパス第1要素を board 名として返します。
// 不正な board (空・想定外の文字) は空文字を返します。
func fourChanBoardFromPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	board := strings.ToLower(parts[0])
	if !fourChanBoardPattern.MatchString(board) {
		return ""
	}
	return board
}

// Prepare は 4chan の対象 URL を検証します。board は保持しません。
func (a *FourChanAdapter) Prepare(_ *network.Client, taskConfig config.Task) error {
	u, err := url.Parse(taskConfig.TargetBoardURL)
	if err != nil {
		return fmt.Errorf("4chan: 対象URLの解析に失敗しました (%s): %w", taskConfig.TargetBoardURL, err)
	}
	switch strings.ToLower(u.Hostname()) {
	case "boards.4chan.org", "boards.4channel.org":
	default:
		return fmt.Errorf("4chan: 対象URLのホストは boards.4chan.org または boards.4channel.org である必要があります: %s", taskConfig.TargetBoardURL)
	}
	if fourChanBoardFromPath(taskConfig.TargetBoardURL) == "" {
		return fmt.Errorf("4chan: 板名を URL のパスから取得できません: %s", taskConfig.TargetBoardURL)
	}
	return nil
}

// BuildCatalogURL は板 URL からカタログ JSON の URL を構築します。
func (a *FourChanAdapter) BuildCatalogURL(baseURL string) (string, error) {
	board := fourChanBoardFromPath(baseURL)
	if board == "" {
		return "", fmt.Errorf("4chan: 板名を URL のパスから取得できません: %s", baseURL)
	}
	return fmt.Sprintf("%s/%s/catalog.json", fourChanAPIBase, board), nil
}

// ParseCatalog は SiteAdapter の要件を満たすための実装です。
// board を URL から導出できないため、コアは ParseCatalogWithBoard を使います。
func (a *FourChanAdapter) ParseCatalog(_ []byte) ([]model.ThreadInfo, error) {
	return nil, fmt.Errorf("4chan: ParseCatalog は board を特定できないため使用できません (BoardScopedCatalogParser 経由で呼び出してください)")
}

// ParseCatalogWithBoard は catalog.json を解析します。
// catalogURL (https://a.4cdn.org/<board>/catalog.json) から board を導出します。
func (a *FourChanAdapter) ParseCatalogWithBoard(catalogURL string, body []byte) ([]model.ThreadInfo, error) {
	board := fourChanBoardFromPath(catalogURL)
	if board == "" {
		return nil, fmt.Errorf("4chan: カタログURLから板名を取得できません: %s", catalogURL)
	}

	// ルートは {page, threads} のオブジェクト配列。
	var pages []fourChanCatalogPage
	if err := json.Unmarshal(body, &pages); err != nil {
		return nil, fmt.Errorf("4chan: カタログJSONの解析に失敗しました: %w", err)
	}

	threads := make([]model.ThreadInfo, 0, len(pages))
	seen := make(map[int64]bool)
	for _, page := range pages {
		for _, t := range page.Threads {
			if t.No == 0 || seen[t.No] {
				continue
			}
			seen[t.No] = true

			// images は OP 添付を含まないため、OP に tim/ext があれば +1 する。
			mediaCount := t.Images
			if t.Tim != 0 && t.Ext != "" {
				mediaCount++
			}

			threads = append(threads, model.ThreadInfo{
				ID:           strconv.FormatInt(t.No, 10),
				Title:        fourChanThreadTitle(t.Sub, t.Com),
				SearchText:   fourChanSearchText(t.Sub, t.Com),
				MediaCount:   mediaCount,
				URL:          fmt.Sprintf("%s/%s/thread/%d.json", fourChanAPIBase, board, t.No),
				ResCount:     t.Replies,
				Date:         fourChanUnixTime(t.LastModified),
				LastModified: t.LastModified,
			})
		}
	}
	return threads, nil
}

// ParseThreadHTML は thread JSON を検証して、そのまま文字列で返します。
// 後続の ExtractMediaFiles / ReconstructHTML が JSON として解釈します。
func (a *FourChanAdapter) ParseThreadHTML(htmlBody []byte) (string, error) {
	var th fourChanThread
	if err := json.Unmarshal(htmlBody, &th); err != nil {
		return "", fmt.Errorf("4chan: スレッドJSONの解析に失敗しました: %w", err)
	}
	if len(th.Posts) == 0 {
		return "", fmt.Errorf("4chan: スレッドJSONに posts がありません")
	}
	return string(htmlBody), nil
}

// ExtractMediaFiles は thread JSON からメディアを抽出します。
// board は threadURL (https://a.4cdn.org/<board>/thread/<no>.json) から導出します。
// 4chan の添付に加え、本文中の catbox (files/litter) のメディア URL も対象にします。
func (a *FourChanAdapter) ExtractMediaFiles(htmlContent string, threadURL string) ([]model.MediaInfo, error) {
	board := fourChanBoardFromPath(threadURL)
	if board == "" {
		return nil, fmt.Errorf("4chan: スレッドURLから板名を取得できません: %s", threadURL)
	}

	var th fourChanThread
	if err := json.Unmarshal([]byte(htmlContent), &th); err != nil {
		return nil, fmt.Errorf("4chan: スレッドJSONの解析に失敗しました: %w", err)
	}

	var media []model.MediaInfo
	seen := make(map[string]bool)
	add := func(rawURL, thumbURL string, resNo int, originalFilename string) {
		if rawURL == "" || seen[rawURL] {
			return
		}
		seen[rawURL] = true
		media = append(media, model.MediaInfo{
			URL:              rawURL,
			ThumbnailURL:     thumbURL,
			OriginalFilename: originalFilename,
			ResNumber:        resNo,
		})
	}

	for _, post := range th.Posts {
		if post.Tim != 0 && post.Ext != "" {
			ext := post.Ext
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			filename := fmt.Sprintf("%d%s", post.Tim, ext)
			add(
				fmt.Sprintf("%s/%s/%s", fourChanCDNBase, board, filename),
				fmt.Sprintf("%s/%s/%ds.jpg", fourChanCDNBase, board, post.Tim),
				post.No,
				filename,
			)
		}

		for _, external := range fourChanExternalMediaURLs(post.Com) {
			u, err := url.Parse(external)
			if err != nil {
				continue
			}
			add(u.String(), "", post.No, path.Base(u.Path))
		}
	}

	return media, nil
}

// ReconstructHTML は thread JSON から閲覧用 HTML を生成します。
// com の既存マークアップ (<br> や引用リンク) は保持し、メディア URL だけを
// ローカルパス (img/...) に置き換えます。CSS は 4chan 専用の最小スタイルです。
func (a *FourChanAdapter) ReconstructHTML(htmlContent string, thread model.ThreadInfo, mediaFiles []model.MediaInfo) (string, error) {
	var th fourChanThread
	if err := json.Unmarshal([]byte(htmlContent), &th); err != nil {
		return "", fmt.Errorf("4chan: スレッドJSONの解析に失敗しました: %w", err)
	}

	byPost := make(map[int][]model.MediaInfo)
	for _, m := range mediaFiles {
		byPost[m.ResNumber] = append(byPost[m.ResNumber], m)
	}

	title := thread.Title
	if title == "" {
		title = "(no title)"
	}

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html><head><meta charset=\"UTF-8\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(title))
	b.WriteString(`<style>
body{background:#eef2ff;color:#111;font-family:sans-serif;margin:0;padding:1em;}
h1{font-size:1.2em;margin:0 0 1em;}
.post{background:#d6daf0;border:1px solid #b7c5d9;margin:0 0 1em;padding:.5em;}
.post .meta{font-size:.85em;color:#34345c;margin-bottom:.3em;}
.post .files{margin-top:.4em;}
.post .files img{max-width:180px;max-height:180px;vertical-align:top;margin:0 .3em .3em 0;}
.post .files a{word-break:break-all;}
.replies{border-left:2px solid #b7c5d9;padding-left:.6em;}
</style></head><body>
`)
	fmt.Fprintf(&b, "<h1>%s</h1>\n<div class=\"thread\">\n", html.EscapeString(title))

	for _, post := range th.Posts {
		fmt.Fprintf(&b, "<div class=\"post\" id=\"p%d\">\n", post.No)
		meta := "Anonymous"
		if post.Name != "" {
			meta = post.Name
		}
		if post.Time != 0 {
			meta += " " + time.Unix(post.Time, 0).Format("2006/01/02 15:04")
		}
		meta += fmt.Sprintf(" No.%d", post.No)
		if post.ID != "" {
			meta += " ID:" + post.ID
		}
		fmt.Fprintf(&b, "<div class=\"meta\">%s</div>\n", html.EscapeString(meta))

		body := post.Com
		if post.Sub != "" {
			body = "<span class=\"subject\">" + html.EscapeString(fourChanCleanText(post.Sub)) + "</span><br>" + body
		}
		b.WriteString("<div class=\"body\">")
		b.WriteString(fourChanRewriteMediaURLs(body, mediaFiles))
		b.WriteString("</div>\n")

		if files := byPost[post.No]; len(files) > 0 {
			b.WriteString("<div class=\"files\">")
			for _, m := range files {
				img := localMediaName(m.LocalPath, m.OriginalFilename)
				if m.ThumbnailURL != "" {
					thumb := localMediaName(m.LocalThumbPath, "")
					if thumb == "" {
						thumb = img
					}
					fmt.Fprintf(&b, "<a href=\"img/%s\"><img src=\"thumb/%s\" alt=\"%s\"></a>",
						html.EscapeString(img), html.EscapeString(thumb), html.EscapeString(img))
				} else {
					// catbox などサムネイルが無いものはテキストリンクにする。
					fmt.Fprintf(&b, "<a href=\"img/%s\">%s</a> ", html.EscapeString(img), html.EscapeString(img))
				}
			}
			b.WriteString("</div>\n")
		}
		b.WriteString("</div>\n")
	}

	b.WriteString("</div>\n</body></html>\n")
	return b.String(), nil
}

// fourChanRewriteMediaURLs は com 内のメディア URL をローカルパスに置き換えます。
// HTML エンティティで書かれた URL (&amp;) も対象にします。
func fourChanRewriteMediaURLs(com string, mediaFiles []model.MediaInfo) string {
	for _, m := range mediaFiles {
		img := localMediaName(m.LocalPath, m.OriginalFilename)
		local := "img/" + img
		variants := []string{
			m.URL,
			html.EscapeString(m.URL),
			strings.ReplaceAll(m.URL, "&", "&amp;"),
		}
		for _, v := range variants {
			if v == "" {
				continue
			}
			com = strings.ReplaceAll(com, v, local)
		}
	}
	return com
}

// localMediaName は保存ファイル名を返します (LocalPath があればその base)。
func localMediaName(localPath, originalFilename string) string {
	if localPath != "" {
		if base := filepath.Base(localPath); base != "" && base != "." {
			return base
		}
	}
	return originalFilename
}

// fourChanExternalMediaURLs は本文中の catbox メディア URL を抽出します。
func fourChanExternalMediaURLs(com string) []string {
	decoded := html.UnescapeString(com)
	var out []string
	for _, raw := range fourChanURLPattern.FindAllString(decoded, -1) {
		raw = strings.TrimRight(raw, ").,;:!?")
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		if !catboxHosts[strings.ToLower(u.Hostname())] {
			continue
		}
		if !externalMediaExts[strings.ToLower(path.Ext(u.Path))] {
			continue
		}
		out = append(out, u.String())
	}
	return out
}

// fourChanCleanText は HTML タグを除き、エンティティをデコードして空白を整えます。
func fourChanCleanText(s string) string {
	s = html.UnescapeString(s)
	s = htmlTagPattern.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// fourChanThreadTitle は sub をタイトルにし、無ければ com の冒頭を使います。
func fourChanThreadTitle(sub, com string) string {
	if t := fourChanCleanText(sub); t != "" {
		return t
	}
	t := fourChanCleanText(com)
	if t == "" {
		return "(no title)"
	}
	r := []rune(t)
	if len(r) > 80 {
		return string(r[:80])
	}
	return t
}

// fourChanSearchText は一次フィルタ用に sub と com を連結した文字列を返します。
func fourChanSearchText(sub, com string) string {
	parts := make([]string, 0, 2)
	if t := fourChanCleanText(sub); t != "" {
		parts = append(parts, t)
	}
	if t := fourChanCleanText(com); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

// fourChanUnixTime は Unix 秒を time.Time に変換します (0 は現在時刻)。
func fourChanUnixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Now()
	}
	return time.Unix(sec, 0)
}
