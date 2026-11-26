package core

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"
)

// DownloadMediaFiles は、指定されたメディアファイルリストをダウンロードします。
// thread_archiver.go からロジックを分離・リファクタリングしたものです。
func DownloadMediaFiles(ctx context.Context, client *network.Client, task config.Task, thread model.ThreadInfo,
	filesToDownload []model.MediaInfo, imgSavePath string, thumbSavePath string, resumeFilePath string, logger *log.Logger) (int, int64, error) {

	// ベースURLを一度パースしておく
	baseURL, err := url.Parse(task.TargetBoardURL)
	if err != nil {
		return 0, 0, fmt.Errorf("ベースURLの解析に失敗しました (url=%s): %w", task.TargetBoardURL, err)
	}

	// レジューム処理の開始ログは一度だけ出力
	if task.EnableResumeSupport {
		if _, err := os.Stat(resumeFilePath); err == nil {
			logger.Printf("INFO: レジューム処理: .resume.jsonから %d 件の未完了ファイルを読み込みました。", len(filesToDownload))
		}
	}

	// 統計情報の初期化
	totalDownloadedFiles := 0
	totalBytesWritten := int64(0)

	for i := range filesToDownload {
		media := &filesToDownload[i]

		downloadedCount, bytesWritten := processMediaItem(ctx, client, task, thread, media, baseURL, imgSavePath, thumbSavePath, logger, i, len(filesToDownload))

		totalDownloadedFiles += downloadedCount
		totalBytesWritten += bytesWritten

		// 少なくとも1つのファイル（本体またはサムネイル）がダウンロードできたらレジューム更新
		if downloadedCount > 0 && task.EnableResumeSupport {
			if err := updateResumeFile(resumeFilePath, media.URL); err != nil {
				logger.Printf("WARNING: レジュームファイルの更新に失敗しました: %v", err)
			}
		}

		time.Sleep(time.Duration(task.RequestIntervalMillis) * time.Millisecond)
	}
	return totalDownloadedFiles, totalBytesWritten, nil
}

// processMediaItem は単一のメディア項目（画像＋サムネイル）を処理します。
// 戻り値: (ダウンロード成功数, 書き込みバイト数)
func processMediaItem(ctx context.Context, client *network.Client, task config.Task, thread model.ThreadInfo,
	media *model.MediaInfo, baseURL *url.URL, imgSavePath, thumbSavePath string, logger *log.Logger, index, total int) (int, int64) {

	var downloadedCount int
	var bytesWritten int64

	// 1. フルサイズ画像の保存パス決定
	saveFileName, err := generateFileName(task.FilenameFormat, thread, *media)
	if err != nil || saveFileName == "" {
		// fallback: 元のファイル名を使用
		saveFileName = media.OriginalFilename
		if saveFileName == "" {
			// さらにfallback: URLからファイル名を抽出
			saveFileName = filepath.Base(media.URL)
			logger.Printf("WARNING: ファイル名の生成に失敗したため、URLから抽出したファイル名を使用します: %s", saveFileName)
		}
	}
	saveFilePath := filepath.Join(imgSavePath, saveFileName)
	media.LocalPath = saveFilePath

	// 2. フルサイズ画像のURL解決
	fullMediaURL := resolveURL(baseURL, media.URL)

	// 3. フルサイズ画像のダウンロード
	logger.Printf("Downloading (%d/%d): %s -> %s", index+1, total, fullMediaURL, saveFileName)
	err = downloadFile(ctx, client, fullMediaURL, saveFilePath, task.RetryCount, task.RetryWaitMillis)
	if err != nil {
		logger.Printf("WARNING: ファイルのダウンロードに失敗しました: %s - %v. スキップします。", fullMediaURL, err)
		// 失敗してもサムネイルは試みる
	} else {
		logger.Printf("SUCCESS: ダウンロード完了: %s", saveFileName)
		downloadedCount++
		if fileInfo, err := os.Stat(saveFilePath); err == nil {
			bytesWritten += fileInfo.Size()
		}
	}

	// 4. サムネイルの処理
	if thumbURL := strings.TrimSpace(media.ThumbnailURL); thumbURL != "" {
		thumbName, thumbBytes, err := downloadThumbnail(ctx, client, task, baseURL, thumbURL, saveFileName, thumbSavePath, logger)
		if err != nil {
			logger.Printf("WARNING: サムネイルのダウンロードに失敗しました: %v", err)
		} else {
			downloadedCount++
			bytesWritten += thumbBytes
			media.LocalThumbPath = filepath.Join(thumbSavePath, thumbName)
		}
	}

	return downloadedCount, bytesWritten
}

// downloadThumbnail はサムネイルをダウンロードします。
// 戻り値: (保存ファイル名, 書き込みバイト数, エラー)
func downloadThumbnail(ctx context.Context, client *network.Client, task config.Task, baseURL *url.URL,
	thumbURL, originalFileName, thumbSaveDir string, logger *log.Logger) (string, int64, error) {

	thumbName := filepath.Base(thumbURL)
	if thumbName == "" || thumbName == "." {
		// fallback: 元のファイル名から推測
		ext := filepath.Ext(originalFileName)
		nameWithoutExt := strings.TrimSuffix(originalFileName, ext)
		thumbName = nameWithoutExt + "s.jpg"
		logger.Printf("WARNING: サムネイルファイル名の抽出に失敗したため、推測値を使用します: %s", thumbName)
	}

	thumbSavePath := filepath.Join(thumbSaveDir, thumbName)
	fullThumbURL := resolveURL(baseURL, thumbURL)

	logger.Printf("Downloading thumb: %s -> %s", fullThumbURL, thumbName)
	if err := downloadFile(ctx, client, fullThumbURL, thumbSavePath, task.RetryCount, task.RetryWaitMillis); err != nil {
		return "", 0, err
	}

	logger.Printf("SUCCESS: サムネイルダウンロード完了: %s", thumbName)
	var size int64
	if fileInfo, err := os.Stat(thumbSavePath); err == nil {
		size = fileInfo.Size()
	}
	return thumbName, size, nil
}

// resolveURL は相対URLを絶対URLに変換します。
func resolveURL(base *url.URL, target string) string {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}
	// URLパースエラーを避けるため、targetをパースしてからResolveReferenceする
	targetURL, err := url.Parse(target)
	if err != nil {
		return target // パースできない場合はそのまま返す
	}
	return base.ResolveReference(targetURL).String()
}

// downloadFile は、単一のファイルをダウンロードし、指定されたパスに保存します。
// リトライロジックを含みます。
func downloadFile(ctx context.Context, client *network.Client, url string, destPath string, retryCount int, retryWaitMillis int) error {
	for i := 0; i <= retryCount; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fileContent, err := client.Get(ctx, url)
		if err != nil {
			if httpErr, ok := err.(*network.HTTPError); ok {
				if !httpErr.IsRetryable() {
					log.Printf("ダウンロード失敗（リトライ不可、HTTP %d）: url=%s, error=%v", httpErr.StatusCode, url, err)
					return fmt.Errorf("リトライ不可能なHTTPエラー (status=%d, url=%s): %w", httpErr.StatusCode, url, err)
				}
				log.Printf("ダウンロード失敗（リトライ可能、HTTP %d、試行 %d/%d）: url=%s, error=%v", httpErr.StatusCode, i+1, retryCount+1, url, err)
			} else {
				log.Printf("ダウンロード失敗（ネットワークエラー、試行 %d/%d）: url=%s, error=%v", i+1, retryCount+1, url, err)
			}

			if i < retryCount {
				time.Sleep(time.Duration(retryWaitMillis) * time.Millisecond)
			}
			continue
		}

		// AGENT.md「Atomic Writes」: 一時ファイル書き込み→os.Renameで設置し、
		// 中断時に壊れた（中途半端な）ファイルが残らないようにする。
		if err := writeFileAtomic(destPath, []byte(fileContent)); err != nil {
			log.Printf("ファイル書き込み失敗（試行 %d/%d）: path=%s, error=%v", i+1, retryCount+1, destPath, err)
			if i < retryCount {
				time.Sleep(time.Duration(retryWaitMillis) * time.Millisecond)
			}
			continue
		}

		return nil
	}

	return fmt.Errorf("ダウンロードがリトライ上限に達しました (url=%s)", url)
}

// writeFileAtomic は、宛先と同一ディレクトリの一時ファイルへ書き込んでから
// os.Rename で設置します。os.WriteFile による直接書き込みは、書き込み中断時に
// 途中までのファイルが残り、以後「取得済み」として誤って扱われ続けるため使用しません。
func writeFileAtomic(destPath string, data []byte) (err error) {
	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, filepath.Base(destPath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("一時ファイルの作成に失敗しました: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("一時ファイルへの書き込みに失敗しました: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("一時ファイルのクローズに失敗しました: %w", err)
	}
	if err = os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("一時ファイルの設置(rename)に失敗しました: %w", err)
	}
	return nil
}
