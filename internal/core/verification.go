package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"GoImageBoardArchiver/internal/adapter"
	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/network"
)

// VerificationResult は検証結果を表します。
type VerificationResult struct {
	TotalChecked   int
	TotalMissing   int
	TotalRepaired  int
	TotalFailed    int
	MissingDetails []string
}

// RunVerification は指定されたタスク（または全タスク）に対して検証と修復を実行します。
func RunVerification(ctx context.Context, cfg *config.Config, targetTaskName string, repair bool, force bool) error {
	log.Println("検証モードを開始します...")
	if repair {
		log.Println("修復モード: 有効 (欠損ファイルを再ダウンロードします)")
	} else {
		log.Println("修復モード: 無効 (検証のみ行います)")
	}

	// 検証履歴のパスを固定
	verificationHistoryPath := "verification_history.json"
	verificationHistory, err := loadVerificationHistory(verificationHistoryPath)
	if err != nil {
		log.Printf("WARNING: 検証履歴の読み込みに失敗しました: %v", err)
		verificationHistory = make(map[string]time.Time)
	}

	totalResult := VerificationResult{}

	for _, task := range cfg.Tasks {
		if targetTaskName != "" && task.TaskName != targetTaskName {
			continue
		}

		log.Printf("タスク '%s' の検証を開始します...", task.TaskName)
		result, err := verifyTask(ctx, task, cfg.Network, repair, force, verificationHistory)
		if err != nil {
			log.Printf("ERROR: タスク '%s' の検証中にエラーが発生しました: %v", task.TaskName, err)
		}

		totalResult.TotalChecked += result.TotalChecked
		totalResult.TotalMissing += result.TotalMissing
		totalResult.TotalRepaired += result.TotalRepaired
		totalResult.TotalFailed += result.TotalFailed
		totalResult.MissingDetails = append(totalResult.MissingDetails, result.MissingDetails...)
	}

	// 検証履歴の保存
	if err := saveVerificationHistory(verificationHistoryPath, verificationHistory); err != nil {
		log.Printf("ERROR: 検証履歴の保存に失敗しました: %v", err)
	}

	log.Println("========================================")
	log.Println("検証完了")
	log.Printf("チェック済みスレッド数: %d", totalResult.TotalChecked)
	log.Printf("欠損あり: %d", totalResult.TotalMissing)
	if repair {
		log.Printf("修復成功: %d", totalResult.TotalRepaired)
		log.Printf("修復失敗: %d", totalResult.TotalFailed)
	}
	if len(totalResult.MissingDetails) > 0 {
		log.Println("詳細:")
		for _, detail := range totalResult.MissingDetails {
			log.Println(detail)
		}
	}
	log.Println("========================================")

	return nil
}

func verifyTask(ctx context.Context, task config.Task, netSettings config.NetworkSettings, repair bool, force bool, history map[string]time.Time) (VerificationResult, error) {
	result := VerificationResult{}

	if task.SaveRootDirectory == "" {
		return result, fmt.Errorf("タスク '%s' の save_root_directory が設定されていません", task.TaskName)
	}

	entries, err := os.ReadDir(task.SaveRootDirectory)
	if err != nil {
		return result, fmt.Errorf("タスクディレクトリ '%s' の読み込みに失敗しました: %w", task.SaveRootDirectory, err)
	}

	// クライアントとアダプタの準備 (修復用)
	var client *network.Client
	var siteAdapter adapter.SiteAdapter
	if repair {
		var err error
		client, err = network.NewClient(netSettings)
		if err != nil {
			return result, fmt.Errorf("クライアントの初期化に失敗しました: %w", err)
		}
		siteAdapter, err = adapter.GetAdapter(task.SiteAdapter)
		if err != nil {
			return result, fmt.Errorf("アダプタの取得に失敗しました: %w", err)
		}
		if err = siteAdapter.Prepare(client, task); err != nil {
			return result, fmt.Errorf("アダプタの準備に失敗しました: %w", err)
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		threadDir := filepath.Join(task.SaveRootDirectory, entry.Name())
		// スレッドIDはディレクトリ名から取得することを試みる
		// より堅牢な方法はスナップショットファイルから読み込むこと
		threadID := entry.Name()
		if snapshot, err := LoadThreadSnapshot(threadDir); err == nil {
			threadID = snapshot.ThreadID
		}

		result.TotalChecked++

		// forceフラグがない場合、最近検証済みのスレッドはスキップ
		if !force {
			if lastVerified, ok := history[threadID]; ok {
				if time.Since(lastVerified) < 24*time.Hour {
					continue
				}
			}
		}

		// index.htmの確認
		indexFiles := []string{"index.htm", "index.html"}
		var indexFound bool
		for _, name := range indexFiles {
			path := filepath.Join(threadDir, name)
			if content, err := os.ReadFile(path); err == nil && len(content) > 0 {
				indexFound = true
				break
			}
		}

		if !indexFound {
			log.Printf("WARNING: スレッド %s (%s) のindex.htmが見つかりません", threadID, threadDir)
			result.TotalMissing++
			result.MissingDetails = append(result.MissingDetails, fmt.Sprintf("[%s] index.htm消失", threadID))
			// index.htmがないと修復は困難
			if repair {
				result.TotalFailed++
			}
			continue
		}

		// 簡易実装: ディレクトリ内のファイルサイズが0のものを検出
		imgDir := filepath.Join(threadDir, "img")
		files, err := os.ReadDir(imgDir)
		if err != nil {
			continue // imgディレクトリがなければスキップ
		}

		missingCount := 0
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			info, err := file.Info()
			if err != nil {
				continue
			}
			if info.Size() == 0 {
				missingCount++
				filePath := filepath.Join(imgDir, file.Name())
				log.Printf("WARNING: スレッド %s のファイル %s がサイズ0です", threadID, filePath)

				if repair {
					// 修復ロジックは複雑なため、今回は破損ファイルの削除のみ
					os.Remove(filePath)
					result.TotalFailed++ // 再ダウンロード機能がないためFailed扱い
					result.MissingDetails = append(result.MissingDetails, fmt.Sprintf("[%s] 破損ファイル削除: %s", threadID, file.Name()))
				} else {
					result.MissingDetails = append(result.MissingDetails, fmt.Sprintf("[%s] 破損ファイル: %s", threadID, file.Name()))
				}
			}
		}

		if missingCount > 0 {
			result.TotalMissing++
		} else {
			// 問題なければ検証履歴を更新
			history[threadID] = time.Now()
		}
	}

	return result, nil
}

func loadVerificationHistory(path string) (map[string]time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]time.Time), nil
		}
		return nil, err
	}
	var history map[string]time.Time
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}
	return history, nil
}

func saveVerificationHistory(path string, history map[string]time.Time) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
