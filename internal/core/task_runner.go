// Package core は、GIBAアプリケーションの中核となるビジネスロジックを実装します。
package core

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"GoImageBoardArchiver/internal/adapter"
	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/model"
	"GoImageBoardArchiver/internal/network"
)

// ExecuteTask は、単一のタスクの全ライフサイクルを管理・実行します。
func ExecuteTask(ctx context.Context, task config.Task, globalNetworkSettings config.NetworkSettings, safetyStopMinDiskGB float64, isWatchMode bool) {

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", task.TaskName), log.LstdFlags|log.Ltime)
	logger.Println("タスクを開始します。")

	// --- コンポーネントの初期化 ---
	client, err := network.NewClient(globalNetworkSettings)
	if err != nil {
		logger.Printf("FATAL: ネットワーククライアントの初期化に失敗しました: %v", err)
		return
	}

	siteAdapter, err := adapter.GetAdapter(task.SiteAdapter)
	if err != nil {
		logger.Printf("FATAL: サイトアダプタの取得に失敗しました: %v", err)
		return
	}

	if err := siteAdapter.Prepare(client, task); err != nil {
		logger.Printf("FATAL: サイト固有設定の適用に失敗しました: %v", err)
		return
	}

	firstLoop := true
	for {
		if isWatchMode && !firstLoop {
			interval := time.Duration(task.WatchIntervalMillis) * time.Millisecond
			if interval <= 0 {
				interval = 15 * time.Minute
			}
			logger.Printf("次のチェックまで %v 待機します...", interval)
			select {
			case <-ctx.Done():
				logger.Println("シャットダウンシグナルを受信しました。タスクを終了します。")
				return
			case <-time.After(interval):
			}
		}
		firstLoop = false

		if err := checkDiskSpace(task.SaveRootDirectory, safetyStopMinDiskGB); err != nil {
			logger.Printf("CRITICAL: ディスク空き容量のチェックに失敗しました: %v。タスクを一時停止します。", err)
			continue
		}

		logger.Println("一次フィルタリングを開始します...")
		targetThreads, err := primaryFiltering(ctx, task, client, siteAdapter)
		if err != nil {
			logger.Printf("ERROR: 一次フィルタリングに失敗しました: %v。次のサイクルで再試行します。", err)
			continue
		}

		if len(targetThreads) == 0 {
			logger.Println("新しい対象スレッドは見つかりませんでした。")
			if !isWatchMode {
				break
			}
			continue
		}

		logger.Printf("%d件の新しい対象スレッドが見つかりました。", len(targetThreads))

		var threadWg sync.WaitGroup
		maxConcurrentDownloads := task.MaxConcurrentDownloads
		if maxConcurrentDownloads <= 0 {
			maxConcurrentDownloads = 4
		}
		threadSemaphore := make(chan struct{}, maxConcurrentDownloads)

		for _, th := range targetThreads { // `thread`を`th`に変更
			select {
			case <-ctx.Done():
				logger.Println("シャットダウンシグナルにより、新規スレッドの処理を中止します。")
				goto end_loop
			default:
			}

			threadWg.Add(1)
			threadSemaphore <- struct{}{}

			go func(th model.ThreadInfo) {
				defer threadWg.Done()
				defer func() { <-threadSemaphore }()
				err := ArchiveSingleThread(ctx, client, siteAdapter, task, th, logger)
				if err != nil {
					logger.Printf("ERROR: スレッド %s のアーカイブに失敗しました: %v", th.ID, err)
				}
			}(th)
		}
	end_loop:

		threadWg.Wait()
		logger.Println("今回の実行サイクルが完了しました。")

		if !isWatchMode {
			break
		}
	}

	logger.Println("タスクを終了します。")
}

func primaryFiltering(ctx context.Context, task config.Task, client *network.Client, siteAdapter adapter.SiteAdapter) ([]model.ThreadInfo, error) {
	catalogURL, err := siteAdapter.BuildCatalogURL(task.TargetBoardURL)
	if err != nil {
		return nil, fmt.Errorf("カタログURLの構築に失敗しました (base_url=%s, adapter=%s): %w", task.TargetBoardURL, task.SiteAdapter, err)
	}

	catalogHTMLString, err := client.Get(ctx, catalogURL)
	if err != nil {
		return nil, fmt.Errorf("カタログHTMLの取得に失敗しました (url=%s, task=%s): %w", catalogURL, task.TaskName, err)
	}
	catalogHTML := []byte(catalogHTMLString)

	candidateThreads, err := siteAdapter.ParseCatalog(catalogHTML)
	if err != nil {
		return nil, fmt.Errorf("カタログHTMLの解析に失敗しました (size=%d bytes, task=%s): %w", len(catalogHTML), task.TaskName, err)
	}

	completedHistory, err := loadHistory(task.HistoryFilePath)
	if err != nil {
		return nil, fmt.Errorf("完了履歴の読み込みに失敗しました (history_file=%s, task=%s): %w", task.HistoryFilePath, task.TaskName, err)
	}

	var targetThreads []model.ThreadInfo
	for _, thread := range candidateThreads {
		// デバッグログ: スレッドのタイトル確認
		// log.Printf("DEBUG: 候補スレッド ID=%s, Title='%s'", thread.ID, thread.Title)

		if _, completed := completedHistory[thread.ID]; completed {
			continue
		}

		matchKeyword := task.SearchKeyword == "" || strings.Contains(thread.Title, task.SearchKeyword)
		exclude := containsAny(thread.Title, task.ExcludeKeywords)

		if matchKeyword && !exclude {
			// log.Printf("DEBUG: スレッド %s ('%s') は条件に一致しました。", thread.ID, thread.Title)
			targetThreads = append(targetThreads, thread)
		}
		// else {
		// 	log.Printf("DEBUG: スレッド %s ('%s') は除外されました (Match=%v, Exclude=%v)", thread.ID, thread.Title, matchKeyword, exclude)
		// }
	}

	return targetThreads, nil
}

func loadHistory(path string) (map[string]bool, error) {
	history := make(map[string]bool)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return history, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		history[scanner.Text()] = true
	}
	return history, scanner.Err()
}

func containsAny(s string, substrings []string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func checkDiskSpace(_ string, _ float64) error {
	return nil
}
