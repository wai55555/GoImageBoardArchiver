package core

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"GoImageBoardArchiver/internal/adapter"
	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"
)

// RepairThread は、指定されたスレッドの破損・欠損ファイルを修復（再ダウンロード）します。
// Web上のスレッドが既に存在しない場合は修復できません。
func RepairThread(ctx context.Context, client *network.Client, siteAdapter adapter.SiteAdapter, task config.Task, threadID string, threadDir string, logger *log.Logger) (int, error) {
	logger.Printf("Starting repair for thread: %s", threadID)

	// 1. スレッドURLの構築
	// スレッドURLは通常 task.TargetBoardURL + "res/" + threadID + ".htm" だが、
	// アダプタやサイトによって異なる可能性があるため、簡易的に構築する。
	// 本来はThreadInfoが必要だが、修復時はIDしか分からない場合が多い。
	// ここではふたばちゃんねるの標準的なURL構造を仮定する。
	// TODO: アダプタにURL構築メソッドを追加するのが望ましい
	baseURL, err := url.Parse(task.TargetBoardURL)
	if err != nil {
		return 0, fmt.Errorf("ターゲットボードURLの解析に失敗しました: %w", err)
	}
	threadURL := baseURL.JoinPath("res", threadID+".htm")

	// 2. Web上のスレッドHTMLを取得
	logger.Printf("Fetching thread HTML from: %s", threadURL.String())
	htmlString, err := client.Get(ctx, threadURL.String())
	if err != nil {
		return 0, fmt.Errorf("スレッドHTMLの取得に失敗しました (404の可能性があります): %w", err)
	}

	// 3. HTML解析とメディア抽出
	htmlContent, err := siteAdapter.ParseThreadHTML([]byte(htmlString))
	if err != nil {
		return 0, fmt.Errorf("スレッドHTMLの解析に失敗しました: %w", err)
	}

	mediaFiles, err := siteAdapter.ExtractMediaFiles(htmlContent, threadURL.String())
	if err != nil {
		return 0, fmt.Errorf("メディアファイルの抽出に失敗しました: %w", err)
	}

	// 4. 修復対象（欠損・破損）ファイルの特定
	imgSavePath := filepath.Join(threadDir, "img")
	thumbSavePath := filepath.Join(threadDir, "thumb")
	
	// ディレクトリがない場合は作成
	if err := os.MkdirAll(imgSavePath, 0755); err != nil {
		return 0, fmt.Errorf("imgディレクトリの作成に失敗しました: %w", err)
	}
	if err := os.MkdirAll(thumbSavePath, 0755); err != nil {
		return 0, fmt.Errorf("thumbディレクトリの作成に失敗しました: %w", err)
	}

	var filesToDownload []model.MediaInfo

	for _, media := range mediaFiles {
		// ファイル名の決定（ArchiveSingleThreadと同様のロジックが必要だが、
		// ここでは簡易的にURLのベース名または既存ファイル名を使用）
		// 本来は generateFileName を使うべきだが、ThreadInfoが不完全なため
		// URLベースでチェックを行う。
		
		filename := filepath.Base(media.URL)
		localPath := filepath.Join(imgSavePath, filename)
		
		// フルサイズ画像のチェック
		if shouldDownload(localPath) {
			logger.Printf("Found missing/corrupted file: %s", filename)
			// LocalPathを設定しておく
			media.LocalPath = localPath
			filesToDownload = append(filesToDownload, media)
		}

		// サムネイルのチェック（サムネイルURLがある場合）
		if media.ThumbnailURL != "" {
			thumbName := filepath.Base(media.ThumbnailURL)
			// ふたばのサムネイル命名規則補正 (s.jpg)
			if filepath.Ext(thumbName) == ".jpg" && len(thumbName) > 5 && thumbName[len(thumbName)-5] != 's' {
				// 念のため
			}
			
			localThumbPath := filepath.Join(thumbSavePath, thumbName)
			if shouldDownload(localThumbPath) {
				// サムネイルのみ欠損している場合も、filesToDownloadに追加するが、
				// downloadMediaFilesはフルサイズもダウンロードしようとする。
				// 効率は悪いが、既存ファイルチェックがdownloadMediaFiles内にはないため、
				// 重複ダウンロードになる可能性がある。
				// しかし、上記でフルサイズが「存在すれば」filesToDownloadに入らないので、
				// ここで追加するとフルサイズも再ダウンロードされてしまう。
				// 
				// 解決策: downloadMediaFiles は渡されたリストを全てダウンロードしようとする。
				// サムネイルだけない場合、MediaInfoを複製してThumbnailURLだけセットする手もあるが、
				// 複雑になるため、今回は「フルサイズがあればフルサイズも上書き覚悟でダウンロード」
				// あるいは「フルサイズがあるならサムネイル欠損は無視（次回のアーカイブで直るかも）」
				// とする。
				// 
				// ここでは「フルサイズが無事ならサムネイル欠損は軽微として無視」せず、
				// 厳密にやるならリストに追加する。ただし既存ファイルの上書きになる。
				// downloadFile関数は「既存ファイルを削除してリトライ」するので、
				// 正常なフルサイズ画像も再ダウンロードされることになる。
				// 帯域の無駄だが、修復としては正しい挙動。
				
				// 既にリストに入っていなければ追加
				alreadyAdded := false
				for _, f := range filesToDownload {
					if f.URL == media.URL {
						alreadyAdded = true
						break
					}
				}
				if !alreadyAdded {
					logger.Printf("Found missing/corrupted thumbnail: %s", thumbName)
					media.LocalPath = localPath // フルサイズのパスも必要
					media.LocalThumbPath = localThumbPath
					filesToDownload = append(filesToDownload, media)
				}
			}
		}
	}

	if len(filesToDownload) == 0 {
		logger.Println("No files need repair.")
		return 0, nil
	}

	logger.Printf("Starting re-download of %d files...", len(filesToDownload))

	// 5. 再ダウンロード実行
	// ダミーのThreadInfoを作成（ファイル名生成に使われる可能性があるが、
	// downloadMediaFiles内でURLからファイル名を推測するfallbackがあるため動作するはず）
	dummyThread := model.ThreadInfo{
		ID:    threadID,
		Title: "Repair " + threadID,
	}

	// resumeFilePathは修復モードでは使わないので空文字または一時ファイル
	resumePath := filepath.Join(threadDir, ".repair_resume.json")

	downloaded, _, err := DownloadMediaFiles(ctx, client, task, dummyThread, filesToDownload, imgSavePath, thumbSavePath, resumePath, logger)
	
	// 一時的なレジュームファイルは削除
	os.Remove(resumePath)

	if err != nil {
		return downloaded, fmt.Errorf("修復中のダウンロードに失敗しました: %w", err)
	}

	return downloaded, nil
}

// shouldDownload は、ファイルが存在しない、またはサイズが0の場合にtrueを返します。
func shouldDownload(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		// その他のエラー（権限など）の場合は、安全のためダウンロード対象としない、
		// あるいはログを出してスキップするが、ここでは簡易的にfalse
		return false
	}
	return info.Size() == 0
}
