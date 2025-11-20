package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/core"
	"GoImageBoardArchiver/internal/systray"
)

// グローバル変数
var (
	// ログファイル管理用
	logFile *os.File

	// コマンドラインフラグ
	configFile *string
	cliMode    *bool
	watchMode  *bool
	verifyMode *bool
	repairMode *bool
	forceMode  *bool
)

func init() {
	configFile = flag.String("config", "config.json", "設定ファイルのパス")
	cliMode = flag.Bool("cli", false, "CLIモードで一度だけ実行します。")
	watchMode = flag.Bool("watch", false, "CLI監視モードで実行します。")
	verifyMode = flag.Bool("verify", false, "検証モードで実行")
	repairMode = flag.Bool("repair", false, "検証モード時に修復を試みる")
	forceMode = flag.Bool("force", false, "検証モード時に全スレッドを強制チェックする")
}

// main関数はGIBAアプリケーションのエントリーポイントです。
func main() {
	// --- フラグの定義 ---
	// (グローバル変数で定義済み)
	flag.Parse()

	// --- ログファイルの設定 ---
	// (setupLoggerで設定されるため、ここでは何もしないが、初期化前にエラーが出るのを防ぐため標準出力にしておく)
	log.SetOutput(os.Stdout)

	// 設定ファイルの読み込み
	cfg, err := config.LoadAndResolve(*configFile)
	if err != nil {
		log.Fatalf("設定ファイルの読み込みに失敗しました: %v", err)
	}
	setupLogger(cfg)

	// モード分岐
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// シグナルハンドリング
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("終了シグナルを受信しました。シャットダウンを開始します...")
		cancel()
	}()

	if *verifyMode {
		// runVerificationModeの引数を修正: (ctx, cfg, targetTaskName, repair, force)
		// targetTaskNameは現状フラグがないので空文字
		runVerificationMode(ctx, cfg, "", *repairMode, *forceMode)
	} else if *cliMode {
		runCliMode(ctx, cfg, *watchMode)
	} else {
		log.Println("実行モード: システムトレイ (デフォルト)")
		runSystrayMode(ctx)
	}

	log.Println("アプリケーションが正常にシャットダウンしました。")
}

func runVerificationMode(ctx context.Context, cfg *config.Config, targetTaskName string, repair bool, force bool) {
	log.Println("検証モードで起動します。")
	if err := core.RunVerification(ctx, cfg, targetTaskName, repair, force); err != nil {
		log.Printf("検証中にエラーが発生しました: %v", err)
		os.Exit(1)
	}
	log.Println("検証モードを終了します。")
}

func runSystrayMode(ctx context.Context) {
	hideConsole()
	systray.RunSystrayApp(ctx, showConsole, hideConsole, toggleLogger)
}

// setupLogger はログ出力先を設定します。
// config.EnableLogFile が true の場合、ファイルにも出力します。
func setupLogger(cfg *config.Config) {
	toggleLogger(cfg.EnableLogFile, cfg.LogFilePath)
}

// toggleLogger はログ出力のファイル書き込みを切り替えます。
// enable: trueならファイルにも出力、falseなら標準出力のみ
// path: ログファイルのパス (enable=trueの場合に必要)
func toggleLogger(enable bool, path string) error {
	// 既存のログファイルがあれば閉じる
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}

	if enable {
		if path == "" {
			// デフォルトは日付形式
			today := time.Now().Format("2006-01-02")
			path = fmt.Sprintf("giba_%s.log", today)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("ログファイルを開けませんでした: %v", err)
			return err
		}
		logFile = f
		// 標準出力とファイルの両方に出力
		mw := io.MultiWriter(os.Stdout, f)
		log.SetOutput(mw)
		log.Printf("ログ出力をファイル '%s' に開始しました", path)
	} else {
		// 標準出力のみに戻す
		log.SetOutput(os.Stdout)
		log.Println("ログ出力を標準出力のみに切り替えました")
	}
	return nil
}

// runCliModeは、CLIモードでの実行ロジックを担当します。
func runCliMode(ctx context.Context, cfg *config.Config, isWatch bool) {
	// ログ設定
	setupLogger(cfg)

	log.Printf("CLIモードを開始します (監視モード: %v)", isWatch)

	tasks := cfg.Tasks
	if len(tasks) == 0 {
		log.Println("有効なタスクがありません。終了します。")
		return
	}

	// 並行実行数の制限 (グローバル設定)
	maxConcurrent := cfg.GlobalMaxConcurrentTasks
	if maxConcurrent <= 0 {
		maxConcurrent = 1 // デフォルト
	}
	taskSemaphore := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	log.Printf("タスク数: %d, 最大並行数: %d", len(tasks), maxConcurrent)

loop:
	for _, task := range tasks {
		select {
		case <-ctx.Done():
			log.Println("コンテキストがキャンセルされたため、新規タスクの開始を中断します。")
			break loop
		default:
			// 続行
		}

		wg.Add(1)
		taskSemaphore <- struct{}{}

		// task変数をgoroutineに渡すためにコピー
		taskCopy := task

		go func() {
			defer func() { <-taskSemaphore }() // セマフォを解放
			defer wg.Done()                    // WaitGroupカウンタを減らす

			// コピーした変数 `taskCopy` を使う
			core.ExecuteTask(ctx, taskCopy, cfg.Network, cfg.SafetyStopMinDiskGB, isWatch, nil)
		}()
	}
	wg.Wait()
	log.Println("全てのCLIタスクが完了しました。")
}

// setupLogFileは、日付ごとのログファイルを作成し、標準出力とファイルの両方に出力するように設定します。
func setupLogFile() *os.File {
	// 現在の日付でログファイル名を生成
	today := time.Now().Format("2006-01-02")
	logFileName := fmt.Sprintf("giba_%s.log", today)

	// ログファイルを開く（追記モード）
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("WARNING: ログファイルの作成に失敗しました: %v", err)
		return nil
	}

	// 標準出力とファイルの両方に出力
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)

	log.Printf("INFO: ログファイルを作成しました: %s", logFileName)
	return logFile
}
