// Package core は、GIBAアプリケーションの中核となるビジネスロジックを実装します。
package core

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"

	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"GoImageBoardArchiver/internal/adapter"
	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"

	"github.com/PuerkitoBio/goquery"
)

// ArchiveSingleThread は、仕様書 STEP 2-5 に基づき、単一のスレッドを完全にアーカイブします。
func ArchiveSingleThread(ctx context.Context, client *network.Client, siteAdapter adapter.SiteAdapter, task config.Task, thread model.ThreadInfo, logger *log.Logger) error {
	logger.Printf("Processing thread: %s (%s)", thread.ID, thread.Title)

	// STEP 1: ディレクトリ構造の準備
	threadSavePath, err := generateDirectoryPath(task.SaveRootDirectory, task.DirectoryFormat, thread)
	if err != nil {
		return fmt.Errorf("failed to generate save path: %w", err)
	}
	imgSavePath := filepath.Join(threadSavePath, "img")
	thumbSavePath := filepath.Join(threadSavePath, "thumb")
	cssSavePath := filepath.Join(threadSavePath, "css")

	if err := os.MkdirAll(imgSavePath, 0755); err != nil {
		return fmt.Errorf("failed to create img directory: %w", err)
	}
	if err := os.MkdirAll(thumbSavePath, 0755); err != nil {
		return fmt.Errorf("failed to create thumb directory: %w", err)
	}
	if err := os.MkdirAll(cssSavePath, 0755); err != nil {
		return fmt.Errorf("failed to create css directory: %w", err)
	}

	// futaba.css を css/ にコピー（手元にある前提）
	cssSource := "css/futaba.css" // プロジェクトルートに置いてある静的ファイル
	cssDest := filepath.Join(cssSavePath, "futaba.css")
	if err := copyFile(cssSource, cssDest); err != nil {
		logger.Printf("WARNING: futaba.cssのコピーに失敗しました: %v", err)
	}

	// STEP 2: スレッドHTMLの取得と二次フィルタリング
	threadURL, err := url.Parse(task.TargetBoardURL)
	if err != nil {
		return fmt.Errorf("failed to parse target board URL: %w", err)
	}
	threadURL = threadURL.JoinPath(thread.URL)

	threadHTMLString, err := client.Get(ctx, threadURL.String())
	if err != nil {
		return fmt.Errorf("failed to get thread HTML: %w", err)
	}
	threadHTML := []byte(threadHTMLString)

	document, err := siteAdapter.ParseThreadHTML(threadHTML)
	if err != nil {
		return fmt.Errorf("failed to parse thread HTML: %w", err)
	}

	if passes, reason := applyPostContentFilters(document, task.PostContentFilters); !passes {
		logger.Printf("Skipped by secondary filter: %s. Reason: %s", thread.ID, reason)
		return nil
	}

	mediaFiles, err := siteAdapter.ExtractMediaFiles(document, threadURL.String())
	if err != nil {
		return fmt.Errorf("failed to extract media files: %w", err)
	}
	if len(mediaFiles) < task.MinimumMediaCount {
		logger.Printf("Skipped: media count %d is less than minimum %d.", len(mediaFiles), task.MinimumMediaCount)
		return nil
	}

	// STEP 3: レジューム処理
	resumeFilePath := filepath.Join(threadSavePath, ".resume.json")
	filesToDownload, err := handleResumeLogic(task.EnableResumeSupport, resumeFilePath, mediaFiles, imgSavePath)
	if err != nil {
		return fmt.Errorf("failed to handle resume logic: %w", err)
	}

	// STEP 4: メディアファイルのダウンロード
	if len(filesToDownload) > 0 {
		logger.Printf("Starting media download. Files to download: %d", len(filesToDownload))
		if err := downloadMediaFiles(ctx, client, task, thread, filesToDownload, imgSavePath, thumbSavePath, resumeFilePath, logger); err != nil {
			return err
		}
	}

	// ---- LocalPath/LocalThumbPath を mediaFiles に同期 ----
	urlToLocal := make(map[string]model.MediaInfo, len(filesToDownload))
	for _, m := range filesToDownload {
		urlToLocal[m.URL] = m
	}
	for i := range mediaFiles {
		if updated, ok := urlToLocal[mediaFiles[i].URL]; ok {
			mediaFiles[i].LocalPath = updated.LocalPath
			mediaFiles[i].LocalThumbPath = updated.LocalThumbPath
		}
		if mediaFiles[i].LocalPath == "" {
			base := filepath.Base(mediaFiles[i].URL)
			mediaFiles[i].LocalPath = filepath.Join(imgSavePath, base)
		}
		if mediaFiles[i].ThumbnailURL != "" && mediaFiles[i].LocalThumbPath == "" {
			thumbBase := filepath.Base(mediaFiles[i].ThumbnailURL)
			mediaFiles[i].LocalThumbPath = filepath.Join(thumbSavePath, thumbBase)
		}
	}

	// STEP 5: HTMLの完全な再構成
	logger.Println("Reconstructing HTML...")
	reconstructedHTML, err := siteAdapter.ReconstructHTML(document, thread, mediaFiles)
	if err != nil {
		return fmt.Errorf("failed to reconstruct HTML: %w", err)
	}
	htmlSavePath := filepath.Join(threadSavePath, "index.htm")
	if err := os.WriteFile(htmlSavePath, []byte(reconstructedHTML), 0644); err != nil {
		return fmt.Errorf("failed to save index.htm: %w", err)
	}

	// STEP 6: 完了処理
	if err := appendToHistory(task.HistoryFilePath, thread.ID); err != nil {
		return fmt.Errorf("failed to append to history: %w", err)
	}

	if task.EnableMetadataIndex {
		if err := appendToMetadataIndex(task, thread, mediaFiles, threadSavePath); err != nil {
			logger.Printf("WARNING: Failed to append to metadata index: %v", err)
		}
	}

	if task.EnableResumeSupport {
		os.Remove(resumeFilePath)
	}

	if task.NotifyOnComplete {
		logger.Println("Notification: Archive complete:", thread.Title)
	}

	logger.Printf("Successfully archived thread %s", thread.ID)
	return nil
}

// --- ヘルパー関数群 ---

func downloadMediaFiles(ctx context.Context, client *network.Client, task config.Task, thread model.ThreadInfo,
	filesToDownload []model.MediaInfo, imgSavePath string, thumbSavePath string, resumeFilePath string, logger *log.Logger) error {
	// ベースURLを一度パースしておく
	baseURL, err := url.Parse(task.TargetBoardURL)
	if err != nil {
		return fmt.Errorf("ベースURLの解析に失敗しました: %w", err)
	}

	// レジューム処理の開始ログは一度だけ出力
	if task.EnableResumeSupport {
		if _, err := os.Stat(resumeFilePath); err == nil {
			logger.Printf("INFO: レジューム処理: .resume.jsonから %d 件の未完了ファイルを読み込みました。", len(filesToDownload))
		}
	}

	for i := range filesToDownload {
		media := &filesToDownload[i]

		// フルサイズ画像は img/ に保存
		saveFileName, err := generateFileName(task.FilenameFormat, thread, *media)
		if err != nil || saveFileName == "" {
			saveFileName = media.OriginalFilename
		}
		saveFilePath := filepath.Join(imgSavePath, saveFileName)
		media.LocalPath = saveFilePath

		// サムネイルは thumb/ に保存
		if media.ThumbnailURL != "" {
			thumbName := filepath.Base(media.ThumbnailURL)
			thumbSavePath := filepath.Join(thumbSavePath, thumbName)
			media.LocalThumbPath = thumbSavePath
		}
		// 相対URLを絶対に
		fullMediaURL := media.URL
		if !strings.HasPrefix(fullMediaURL, "http://") && !strings.HasPrefix(fullMediaURL, "https://") {
			resolvedURL := baseURL.ResolveReference(&url.URL{Path: fullMediaURL})
			fullMediaURL = resolvedURL.String()
		}

		logger.Printf("Downloading (%d/%d): %s -> %s", i+1, len(filesToDownload), fullMediaURL, saveFileName)
		err = downloadFile(ctx, client, fullMediaURL, saveFilePath, task.RetryCount, task.RetryWaitMillis)
		if err != nil {
			logger.Printf("WARNING: ファイルのダウンロードに失敗しました: %s - %v. スキップします。", fullMediaURL, err)
			// 失敗してもサムネイルは試みる（フルサイズ欠落でも HTML は表示可能）
		} else {
			logger.Printf("SUCCESS: ダウンロード完了: %s", saveFileName)
			if task.EnableResumeSupport {
				if err := updateResumeFile(resumeFilePath, media.URL); err != nil {
					logger.Printf("WARNING: レジュームファイルの更新に失敗しました: %v", err)
				}
			}
		}

		// ---- サムネイルのダウンロード（存在する場合）----
		if thumbURL := strings.TrimSpace(media.ThumbnailURL); thumbURL != "" {
			thumbName := filepath.Base(thumbURL) // 例: 1763426018532s.jpg
			thumbSaveName := thumbName

			// フォーマットがある場合でも、サムネイルは元の s 付きファイル名で保存する方が整合的
			thumbSavePath := filepath.Join(thumbSavePath, thumbSaveName)
			media.LocalThumbPath = thumbSavePath

			fullThumbURL := thumbURL
			if !strings.HasPrefix(fullThumbURL, "http://") && !strings.HasPrefix(fullThumbURL, "https://") {
				resolvedURL := baseURL.ResolveReference(&url.URL{Path: fullThumbURL})
				fullThumbURL = resolvedURL.String()
			}

			logger.Printf("Downloading thumb: %s -> %s", fullThumbURL, thumbSaveName)
			if err := downloadFile(ctx, client, fullThumbURL, thumbSavePath, task.RetryCount, task.RetryWaitMillis); err != nil {
				logger.Printf("WARNING: サムネイルのダウンロードに失敗しました: %s - %v", fullThumbURL, err)
			} else {
				logger.Printf("SUCCESS: サムネイルダウンロード完了: %s", thumbSaveName)
			}
		}

		time.Sleep(time.Duration(task.RequestIntervalMillis) * time.Millisecond)
	}
	return nil
}

// downloadFile は、単一のファイルをダウンロードし、指定されたパスに保存します。
// リトライロジックを含みます。
func downloadFile(ctx context.Context, client *network.Client, url string, destPath string, retryCount int, retryWaitMillis int) error {
	for i := 0; i <= retryCount; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err() // コンテキストがキャンセルされたら即座に終了
		default:
		}

		fileContent, err := client.Get(ctx, url)
		if err != nil {
			log.Printf("ダウンロード失敗 (リトライ %d/%d): %s - %v", i, retryCount, url, err)
			time.Sleep(time.Duration(retryWaitMillis) * time.Millisecond)
			continue
		}

		if err := os.WriteFile(destPath, []byte(fileContent), 0644); err != nil {
			log.Printf("ファイル書き込み失敗 (リトライ %d/%d): %s - %v", i, retryCount, destPath, err)
			time.Sleep(time.Duration(retryWaitMillis) * time.Millisecond)
			continue
		}

		return nil // ダウンロード成功
	}
	return fmt.Errorf("ダウンロードがリトライ上限に達しました: %s", url)
}

func generateDirectoryPath(rootDir, format string, thread model.ThreadInfo) (string, error) {
	r := strings.NewReplacer(
		"{year}", strconv.Itoa(thread.Date.Year()),
		"{month}", fmt.Sprintf("%02d", thread.Date.Month()),
		"{day}", fmt.Sprintf("%02d", thread.Date.Day()),
		"{thread_id}", thread.ID,
		"{thread_title_safe}", SanitizeFilename(thread.Title),
	)
	return filepath.Join(rootDir, r.Replace(format)), nil
}

func applyPostContentFilters(doc *goquery.Document, filters *config.PostContentFilters) (bool, string) {
	if filters == nil {
		return true, ""
	}
	html, _ := doc.Html()
	text := doc.Text()

	if len(filters.IncludeAnyText) > 0 {
		found := false
		for _, s := range filters.IncludeAnyText {
			if strings.Contains(text, s) {
				found = true
				break
			}
		}
		if !found {
			return false, "does not contain any of the required texts"
		}
	}

	if len(filters.ExcludeAllText) > 0 {
		for _, s := range filters.ExcludeAllText {
			if strings.Contains(text, s) {
				return false, fmt.Sprintf("contains excluded text '%s'", s)
			}
		}
	}

	if len(filters.IncludeAuthorIDs) > 0 {
		found := false
		for _, id := range filters.IncludeAuthorIDs {
			if strings.Contains(html, id) {
				found = true
				break
			}
		}
		if !found {
			return false, "does not contain any of the required author IDs"
		}
	}

	return true, ""
}

// handleResumeLogic は、レジューム処理のロジックを管理します。
// .resume.jsonを読み込み、ディスク上のファイル存在もチェックして、
// 本当にダウンロードが必要なファイルのみのリストを返します。
func handleResumeLogic(enabled bool, resumePath string, allMediaFiles []model.MediaInfo, mediaSavePath string) ([]model.MediaInfo, error) {
	if !enabled {
		return allMediaFiles, nil
	}

	var pendingFilesFromResume []model.MediaInfo
	var finalFilesToDownload []model.MediaInfo

	// .resume.jsonが存在すれば読み込む
	if data, err := os.ReadFile(resumePath); err == nil {
		if json.Unmarshal(data, &pendingFilesFromResume) == nil {
			log.Printf("INFO: レジューム処理: .resume.jsonから %d 件の未完了ファイルを読み込みました。", len(pendingFilesFromResume))
		}
	}

	// pendingFilesFromResumeが空の場合、または読み込みに失敗した場合は、allMediaFilesを初期リストとする
	initialFilesToCheck := allMediaFiles
	if len(pendingFilesFromResume) > 0 {
		initialFilesToCheck = pendingFilesFromResume
	}

	// ディスク上のファイル存在チェック
	for _, media := range initialFilesToCheck {
		saveFileName, err := generateFileName("", model.ThreadInfo{}, media) // threadInfoはファイル名生成に不要なためダミー
		if err != nil {
			log.Printf("WARNING: レジューム処理中のファイル名生成失敗: %s - %v. このファイルをダウンロード対象とします。", media.URL, err)
			finalFilesToDownload = append(finalFilesToDownload, media)
			continue
		}
		saveFilePath := filepath.Join(mediaSavePath, saveFileName)

		if fileInfo, err := os.Stat(saveFilePath); err == nil && fileInfo.Size() > 0 {
			// ファイルが既に存在し、サイズも0より大きい場合はスキップ
			log.Printf("INFO: レジューム処理: ファイルは既に存在します。スキップ: %s", saveFileName)
		} else {
			// ファイルが存在しない、またはサイズが0の場合はダウンロード対象とする
			finalFilesToDownload = append(finalFilesToDownload, media)
		}
	}

	// ダウンロード対象リストで.resume.jsonを更新
	if len(finalFilesToDownload) > 0 {
		data, err := json.MarshalIndent(finalFilesToDownload, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("レジュームファイルの更新に失敗しました: %w", err)
		}
		if err := os.WriteFile(resumePath, data, 0644); err != nil {
			return nil, fmt.Errorf("レジュームファイルの書き込みに失敗しました: %w", err)
		}
	} else {
		// ダウンロード対象がなければレジュームファイルを削除
		os.Remove(resumePath)
	}

	return finalFilesToDownload, nil
}

func generateFileName(format string, thread model.ThreadInfo, media model.MediaInfo) (string, error) {
	if format == "" {
		return media.OriginalFilename, nil // fallback
	}
	r := strings.NewReplacer(
		"{year}", strconv.Itoa(thread.Date.Year()),
		"{month}", fmt.Sprintf("%02d", thread.Date.Month()),
		"{day}", fmt.Sprintf("%02d", thread.Date.Day()),
		"{thread_id}", thread.ID,
		"{res_number}", strconv.Itoa(media.ResNumber),
		"{original_filename}", SanitizeFilename(strings.TrimSuffix(media.OriginalFilename, filepath.Ext(media.OriginalFilename))),
		"{ext}", strings.TrimPrefix(filepath.Ext(media.OriginalFilename), "."),
	)
	return r.Replace(format), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}

	// 書き込みを確実に反映
	return out.Sync()
}

func updateResumeFile(resumePath, downloadedURL string) error {
	data, err := os.ReadFile(resumePath)
	if err != nil {
		return err
	}

	var pendingFiles []model.MediaInfo
	if err := json.Unmarshal(data, &pendingFiles); err != nil {
		return err
	}

	var newPendingFiles []model.MediaInfo
	for _, file := range pendingFiles {
		if file.URL != downloadedURL {
			newPendingFiles = append(newPendingFiles, file)
		}
	}

	newData, err := json.MarshalIndent(newPendingFiles, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(resumePath, newData, 0644)
}

func appendToHistory(path, threadID string) error {
	// スタブ迂回処理
	log.Printf("STUB: appendToHistory called for thread %s, path=%s (skipped)", threadID, path)
	return nil // 本来はファイルに追記するが、今は成功扱い

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(threadID + "\n")
	return err
}

func appendToMetadataIndex(task config.Task, thread model.ThreadInfo, mediaFiles []model.MediaInfo, savePath string) error {
	// スタブ迂回処理
	log.Printf("STUB: appendToMetadataIndex called for thread %s (skipped)", thread.ID)
	return nil

	path := task.MetadataIndexPath
	format := task.MetadataIndexFormat
	if format == "" {
		format = "csv"
	}

	if format != "csv" {
		return fmt.Errorf("unsupported metadata format: %s", format)
	}

	var totalSize int64
	for _, media := range mediaFiles {
		info, err := os.Stat(filepath.Join(filepath.Dir(savePath), media.LocalPath))
		if err == nil {
			totalSize += info.Size()
		}
	}

	record := []string{
		thread.ID,
		thread.Title,
		savePath,
		thread.Date.Format(time.RFC3339),
		strconv.Itoa(len(mediaFiles)),
		fmt.Sprintf("%.2f", float64(totalSize)/1024/1024),
	}

	_, err := os.Stat(path)
	needsHeader := os.IsNotExist(err)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	defer writer.Flush()

	if needsHeader {
		header := []string{"ThreadID", "Title", "SavePath", "Date", "FileCount", "TotalSizeMB"}
		if err := writer.Write(header); err != nil {
			return err
		}
	}

	return writer.Write(record)
}

func SanitizeFilename(name string) string {
	r := strings.NewReplacer(
		"/", "／",
		"\\", "＼",
		":", "：",
		"*", "＊",
		"?", "？",
		"\"", "”",
		"<", "＜",
		">", "＞",
		"|", "｜",
	)
	return r.Replace(name)
}
