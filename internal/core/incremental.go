// Package core は、GIBAアプリケーションの中核となるビジネスロジックを実装します。
package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"GoImageBoardArchiver/internal/model"
)

// ThreadSnapshot は、スレッドの状態スナップショットを表します。
type ThreadSnapshot struct {
	ThreadID       string    `json:"thread_id"`
	LastChecked    time.Time `json:"last_checked"`
	LastPostCount  int       `json:"last_post_count"`
	LastMediaCount int       `json:"last_media_count"`
	LastModified   time.Time `json:"last_modified"`
	IsComplete     bool      `json:"is_complete"` // スレッドが落ちた（404）場合にtrue
}

// LoadThreadSnapshot は、既存のスナップショットファイルを読み込みます。
func LoadThreadSnapshot(threadSavePath string) (*ThreadSnapshot, error) {
	snapshotPath := filepath.Join(threadSavePath, ".snapshot.json")
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // スナップショットが存在しない（初回アーカイブ）
		}
		return nil, fmt.Errorf("スナップショットファイルの読み込みに失敗しました (path=%s): %w", snapshotPath, err)
	}

	var snapshot ThreadSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("スナップショットのパースに失敗しました (path=%s): %w", snapshotPath, err)
	}

	return &snapshot, nil
}

// SaveThreadSnapshot は、スレッドの現在の状態をスナップショットとして保存します。
func SaveThreadSnapshot(threadSavePath string, snapshot *ThreadSnapshot) error {
	snapshotPath := filepath.Join(threadSavePath, ".snapshot.json")
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("スナップショットのシリアライズに失敗しました: %w", err)
	}

	if err := os.WriteFile(snapshotPath, data, 0644); err != nil {
		return fmt.Errorf("スナップショットファイルの書き込みに失敗しました (path=%s): %w", snapshotPath, err)
	}

	return nil
}

// NeedsUpdate は、スレッドが更新されているかどうかを判定します。
func NeedsUpdate(snapshot *ThreadSnapshot, currentMediaCount int) bool {
	if snapshot == nil {
		return true // 初回アーカイブ
	}

	if snapshot.IsComplete {
		return false // 既に完了済み（スレッドが落ちている）
	}

	// メディア数が増えている場合は更新が必要
	if currentMediaCount > snapshot.LastMediaCount {
		return true
	}

	return false
}

// ExtractPostsFromHTML は、HTMLコンテンツからレス情報を抽出します。
// 削除されたレスの検知のために使用します。
func ExtractPostsFromHTML(htmlContent string, mediaFiles []model.MediaInfo) []Post {
	// 簡易的な実装: メディアファイルのResNumberからレス情報を構築
	postMap := make(map[int]Post)

	for _, media := range mediaFiles {
		if _, exists := postMap[media.ResNumber]; !exists {
			postMap[media.ResNumber] = Post{
				ResNumber: media.ResNumber,
				HasMedia:  true,
			}
		}
	}

	// レス番号順にソート
	posts := make([]Post, 0, len(postMap))
	for _, post := range postMap {
		posts = append(posts, post)
	}

	return posts
}

// Post は、単一のレスを表します。
type Post struct {
	ResNumber int  `json:"res_number"`
	HasMedia  bool `json:"has_media"`
}

// detectAndExtractDeletedContent は、旧HTMLと新HTMLを比較して削除されたレスを抽出します。
func detectAndExtractDeletedContent(oldHTML, newHTML, threadID string, logger *log.Logger) string {
	// 簡易的な実装: レス番号（No.XXXXXXXX）のパターンを抽出して比較
	oldResNumbers := extractResNumbers(oldHTML)
	newResNumbers := extractResNumbers(newHTML)

	// 削除されたレス番号を検出
	deletedResNumbers := make([]string, 0)
	for resNum := range oldResNumbers {
		if _, exists := newResNumbers[resNum]; !exists {
			logger.Printf("INFO: 削除されたレスを検知しました (thread_id=%s, res_number=%s)", threadID, resNum)
			deletedResNumbers = append(deletedResNumbers, resNum)
		}
	}

	if len(deletedResNumbers) == 0 {
		return ""
	}

	logger.Printf("INFO: 合計 %d 件のレスが削除されました (thread_id=%s)", len(deletedResNumbers), threadID)

	// 削除されたレスのHTMLを抽出
	deletedHTML := extractPostsHTML(oldHTML, deletedResNumbers)
	return deletedHTML
}

// extractPostsHTML は、指定されたレス番号のHTMLを抽出します。
func extractPostsHTML(html string, resNumbers []string) string {
	var result strings.Builder

	for _, resNum := range resNumbers {
		// ふたばのレス構造: <table>...</table> または <div class="reply">...</div>
		// レス番号を含むブロックを抽出
		patterns := []string{
			// tableベースのレイアウト
			`(?s)<table[^>]*>.*?No\.` + resNum + `.*?</table>`,
			// divベースのレイアウト
			`(?s)<div[^>]*class="[^"]*reply[^"]*"[^>]*>.*?No\.` + resNum + `.*?</div>`,
			// blockquoteを含む場合
			`(?s)<blockquote[^>]*>.*?No\.` + resNum + `.*?</blockquote>`,
		}

		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern)
			matches := re.FindAllString(html, -1)
			for _, match := range matches {
				result.WriteString(match)
				result.WriteString("\n")
			}
		}
	}

	return result.String()
}

// mergeDeletedPostsIntoHTML は、削除されたレスを含む完全版HTMLを生成します。
func mergeDeletedPostsIntoHTML(newHTML, deletedPostsHTML string) (string, error) {
	if deletedPostsHTML == "" {
		// 削除されたレスがない場合は新しいHTMLをそのまま返す
		return newHTML, nil
	}

	// 削除されたレスに「削除済み」マーカーを追加
	markedDeletedPosts := markAsDeleted(deletedPostsHTML)

	// 新しいHTMLに削除されたレスを挿入
	// 戦略: </body>タグの前に削除されたレスセクションを追加
	bodyCloseIndex := strings.LastIndex(newHTML, "</body>")
	if bodyCloseIndex == -1 {
		// </body>が見つからない場合は末尾に追加
		return newHTML + "\n" + createDeletedSection(markedDeletedPosts), nil
	}

	result := newHTML[:bodyCloseIndex] +
		createDeletedSection(markedDeletedPosts) +
		newHTML[bodyCloseIndex:]

	return result, nil
}

// markAsDeleted は、削除されたレスに視覚的なマーカーを追加します。
func markAsDeleted(postsHTML string) string {
	if postsHTML == "" {
		return ""
	}

	// 削除マーカーのスタイルを追加
	deletedStyle := `<div style="background: #ffe0e0; border: 2px solid #ff0000; padding: 10px; margin: 10px 0; opacity: 0.7;">
<div style="color: #ff0000; font-weight: bold; margin-bottom: 5px;">⚠️ このレスは削除されました (削除検知: ` + time.Now().Format("2006-01-02 15:04:05") + `)</div>
`
	deletedStyleClose := `</div>`

	return deletedStyle + postsHTML + deletedStyleClose
}

// createDeletedSection は、削除されたレスのセクションを作成します。
func createDeletedSection(deletedPostsHTML string) string {
	if deletedPostsHTML == "" {
		return ""
	}

	return fmt.Sprintf(`
<!-- 削除されたレスのセクション -->
<hr style="border: 2px dashed #ff0000; margin: 20px 0;">
<div id="deleted-posts-section" style="background: #fff8f8; padding: 20px; margin: 20px 0;">
<h2 style="color: #ff0000;">🗑️ 削除されたレス</h2>
<p style="color: #666;">以下のレスはスレッドから削除されましたが、アーカイブに保存されています。</p>
%s
</div>
`, deletedPostsHTML)
}

// extractResNumbers は、HTMLからレス番号を抽出します。
func extractResNumbers(html string) map[string]bool {
	resNumbers := make(map[string]bool)

	// ふたばのレス番号パターン: "No.1234567890" または data-res="1234567890"
	patterns := []string{
		`No\.(\d+)`,
		`data-res="(\d+)"`,
		`id="r(\d+)"`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(html, -1)
		for _, match := range matches {
			if len(match) > 1 {
				resNumbers[match[1]] = true
			}
		}
	}

	return resNumbers
}
