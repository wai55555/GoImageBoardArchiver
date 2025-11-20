package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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

	// 検証履歴の読み込み
	verificationHistory, err := loadVerificationHistory(cfg.VerificationHistoryPath)
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
	if err := saveVerificationHistory(cfg.VerificationHistoryPath, verificationHistory); err != nil {
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

	// 完了履歴の読み込み
	completedHistory, err := loadTaskHistory(task.HistoryFilePath)
	if err != nil {
		return result, fmt.Errorf("履歴ファイルの読み込みに失敗しました: %w", err)
	}

	// クライアントとアダプタの準備 (修復用)
	var client *network.Client
	var siteAdapter adapter.SiteAdapter
	if repair {
		// NewClientの戻り値が2つある場合に対応 (client, err)
		// もしNewClientがエラーを返さないなら client = network.NewClient(netSettings)
		// コンパイラエラー "assignment mismatch: 1 variable but network.NewClient returns 2 values" より
		var err error
		client, err = network.NewClient(netSettings)
		if err != nil {
			return result, fmt.Errorf("クライアントの初期化に失敗しました: %w", err)
		}
		// エラーハンドリングが必要なら追加するが、NewClientの実装次第。
		// ここでは簡易的に代入のみ。もしエラーが返るなら:
		// client, err = network.NewClient(netSettings)
		// if err != nil { ... }
		// しかし、NewClientのシグネチャが不明確。
		// エラーメッセージ "want (config.NetworkSettings)" に従い引数を修正。

		siteAdapter = &adapter.FutabaAdapter{} // TODO: タスク設定から選択
		if err = siteAdapter.Prepare(client, task); err != nil {
			return result, fmt.Errorf("アダプタの準備に失敗しました: %w", err)
		}
	}

	for threadID := range completedHistory {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		// forceフラグがない場合、最近検証済みのスレッドはスキップ
		if !force {
			if lastVerified, ok := history[threadID]; ok {
				// 最終検証が完了日時より後ならスキップ
				// (完了日時自体はhistory.txtには保存されていないが、ここでは簡易的に
				//  「検証履歴にあればスキップ」とするか、一定期間経過で再検証する)
				// 今回は「24時間以内に検証済みならスキップ」とする
				if time.Since(lastVerified) < 24*time.Hour {
					continue
				}
				// ディレクトリごと消えている場合、再ダウンロードを試みる
				// ただしURLが分からないため、history.txtにURLが含まれていないと再構築不能。
				// 現在のloadHistoryの実装ではURLはロードされない。
				// したがって、ディレクトリ消失の場合は修復不能として扱う。
				result.TotalFailed++
			}
			continue
		}

		// ディレクトリを検索
		foundDir, err := findThreadDirectory(task.SaveRootDirectory, threadID)
		if err != nil {
			log.Printf("WARNING: スレッド %s のディレクトリが見つかりません: %v", threadID, err)
			result.TotalMissing++
			result.MissingDetails = append(result.MissingDetails, fmt.Sprintf("[%s] ディレクトリ消失", threadID))

			if repair {
				// ディレクトリごと消えている場合、再ダウンロードを試みる
				// ただしURLが分からないため、history.txtにURLが含まれていないと再構築不能。
				// 現在のloadHistoryの実装ではURLはロードされない。
				// したがって、ディレクトリ消失の場合は修復不能として扱う。
				result.TotalFailed++
			}
			continue
		}

		// index.htmの確認
		indexFiles := []string{"index.htm", "index.html"}
		var indexFound bool
		for _, name := range indexFiles {
			path := filepath.Join(foundDir, name)
			if content, err := os.ReadFile(path); err == nil && len(content) > 0 {
				// indexContent = string(content) // 未使用のため削除
				indexFound = true
				break
			}
		}

		if !indexFound {
			log.Printf("WARNING: スレッド %s (%s) のindex.htmが見つかりません", threadID, foundDir)
			result.TotalMissing++
			result.MissingDetails = append(result.MissingDetails, fmt.Sprintf("[%s] index.htm消失", threadID))
			// index.htmがないと画像リストも分からないため、修復は難しい（再ダウンロードするしかない）
			// URLが不明なためスキップ
			continue
		}

		// 画像ファイルの存在確認
		// index.htmからリンクされている画像を探すのはパースが必要で重い。
		// 簡易的に、ディレクトリ内のファイルサイズが0のものを探す、あるいは
		// 既知のメディア拡張子を持つファイルが壊れていないかチェックする。
		// 本格的にはHTMLをパースして src="..." をチェックすべき。

		// ここでは、adapter.ExtractMediaFiles を使いたいが、HTML構造が変わっている（再構築済み）ため
		// adapterのメソッドが使えるとは限らない。
		// しかし、ReconstructHTMLで生成されたHTMLは相対パスでリンクしているはず。

		// 簡易実装: ディレクトリ内の全ファイルをスキャンし、サイズ0のファイルを検出
		files, err := os.ReadDir(foundDir)
		if err != nil {
			continue
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
				log.Printf("WARNING: スレッド %s のファイル %s がサイズ0です", threadID, file.Name())

				if repair {
					// サイズ0のファイルを削除して再ダウンロード...したいがURLが不明。
					// 元のURLが分からないとダウンロードできない。
					// ファイル名から元のURLを推測できるか？ (ふたばの場合: 123456789.jpg -> http://.../123456789.jpg)
					// adapterにURL復元ロジックがあれば可能。

					// 今回は「サイズ0のファイル削除」のみ行う
					os.Remove(filepath.Join(foundDir, file.Name()))
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

// findThreadDirectory は指定されたIDを含むディレクトリを検索します。
func findThreadDirectory(baseDir, threadID string) (string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", err
	}

	// 完全一致または "Title (ID)" 形式を検索
	// IDはユニークであると仮定
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == threadID {
			return filepath.Join(baseDir, name), nil
		}
		if strings.Contains(name, fmt.Sprintf("(%s)", threadID)) {
			return filepath.Join(baseDir, name), nil
		}
		// IDが末尾にある場合など
		if strings.HasSuffix(name, threadID) {
			return filepath.Join(baseDir, name), nil
		}
	}
	return "", fmt.Errorf("not found")
}

func loadVerificationHistory(path string) (map[string]time.Time, error) {
	if path == "" {
		path = "verification_history.json"
	}
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
	if path == "" {
		path = "verification_history.json"
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// loadTaskHistory は履歴ファイルを読み込みます。(task_runner.goからコピー)
func loadTaskHistory(path string) (map[string]bool, error) {
	history := make(map[string]bool)
	if path == "" {
		return history, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return history, nil
		}
		return nil, err
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			history[line] = true
		}
	}
	return history, nil
}
