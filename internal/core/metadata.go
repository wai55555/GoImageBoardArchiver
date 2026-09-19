package core

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
)

// metadataMutex はメタデータファイルへの同時書き込みを防ぐためのMutexです。
var metadataMutex sync.Mutex

// AppendToMetadataIndex は、アーカイブ完了後にスレッド情報を metadata.csv に追記します。
func AppendToMetadataIndex(saveRootDir string, task config.Task, thread model.ThreadInfo, mediaFiles []model.MediaInfo, threadSavePath string) error {
	metadataMutex.Lock()
	defer metadataMutex.Unlock()

	filename := "metadata.csv"
	filePath := filepath.Join(saveRootDir, filename)

	// ファイルが存在しない場合はヘッダーを書き込むためのフラグ
	isNewFile := false
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		isNewFile = true
	}

	// 追記モードでファイルを開く
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("メタデータファイルのオープンに失敗しました: %w", err)
	}
	defer f.Close()

	// BOM (Byte Order Mark) for Excel compatibility (UTF-8 with BOM)
	if isNewFile {
		if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return fmt.Errorf("BOMの書き込みに失敗しました: %w", err)
		}
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	// ヘッダーの書き込み
	if isNewFile {
		header := []string{
			"TaskName",
			"ThreadID",
			"Title",
			"URL",
			"Date",
			"MediaCount",
			"LocalPath",
			"ArchivedAt",
		}
		if err := w.Write(header); err != nil {
			return fmt.Errorf("ヘッダーの書き込みに失敗しました: %w", err)
		}
	}

	// レコードの作成
	// 相対パスを計算
	relPath, err := filepath.Rel(saveRootDir, threadSavePath)
	if err != nil {
		relPath = threadSavePath // 失敗した場合は絶対パスのまま
	}

	record := []string{
		task.TaskName,
		thread.ID,
		thread.Title,
		thread.URL,
		thread.Date.Format("2006-01-02 15:04:05"),
		strconv.Itoa(len(mediaFiles)),
		relPath,
		time.Now().Format("2006-01-02 15:04:05"),
	}

	if err := w.Write(record); err != nil {
		return fmt.Errorf("レコードの書き込みに失敗しました: %w", err)
	}

	return nil
}
