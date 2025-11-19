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

// main関数はGIBAアプリケーションのエントリーポイントです。
func main() {
	// --- フラグの定義 ---
	configPath := flag.String("config", "config.json", "設定ファイルのパス")
	cliMode := flag.Bool("cli", false, "CLIモードで一度だけ実行します。")
	watchMode := flag.Bool("watch", false, "CLI監視モードで実行します。")
	verifyTask := flag.String("verify", "", "指定したタスク（または全て）を検証モードで実行します。")
	repairMode := flag.Bool("repair", false, "検証モードでアーカイブの修復を試みます。")
	forceMode := flag.Bool("force", false, "検証モードで検証履歴を無視します。")
	flag.Parse()

	// --- ログファイルの設定 ---
	logFile := setupLogFile()
	if logFile != nil {
		defer logFile.Close()
	}

	// --- ロガーの初期化 ---
	logger := log.New(os.Stdout, "[GIBA-Main] ", log.LstdFlags|log.Lshortfile)
	logger.Println("GoImageBoardArchiver (GIBA) を起動します...")

	// --- Graceful Shutdownのためのコンテキスト ---
	globalCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("終了シグナル (%v) を受信しました。シャットダウンを開始します...", sig)
		cancel()
	}()

	// --- 実行モードの判定とディスパッチ ---
	verifyFlagPassed := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "verify" {
			verifyFlagPassed = true
		}
	})

	if verifyFlagPassed || *repairMode || *forceMode {
		logger.Println("実行モード: 検証")
		runVerificationMode(globalCtx, *configPath, *verifyTask, *repairMode, *forceMode)
	} else if *watchMode {
		logger.Println("実行モード: CLI監視")
		runCliMode(globalCtx, *configPath, true)
	} else if *cliMode {
		logger.Println("実行モード: CLIシングル実行")
		runCliMode(globalCtx, *configPath, false)
	} else {
		logger.Println("実行モード: システムトレイ (デフォルト)")
		runSystrayMode(globalCtx)
	}

	logger.Println("アプリケーションが正常にシャットダウンしました。")
}

func runSystrayMode(ctx context.Context) {
	systray.RunSystrayApp(ctx)
}

// runCliModeは、CLIモードでの実行ロジックを担当します。
func runCliMode(ctx context.Context, configPath string, isWatch bool) {
	logger := log.New(os.Stdout, "[GIBA-CLI] ", log.LstdFlags|log.Lshortfile)
	logger.Println("CLIモードでコアエンジンを起動します...")

	// 1. 設定ファイルの読み込みと解決
	cfg, err := config.LoadAndResolve(configPath)
	if err != nil {
		logger.Fatalf("設定ファイルの読み込みに失敗しました: %v", err)
	}
	log.Printf("設定ファイル(v%s)を '%s' から正常に読み込みました。", cfg.ConfigVersion, configPath)

	// 2. タスクの並行処理
	var wg sync.WaitGroup
	maxConcurrentTasks := cfg.GlobalMaxConcurrentTasks
	if maxConcurrentTasks <= 0 {
		maxConcurrentTasks = 4
	}
	taskSemaphore := make(chan struct{}, maxConcurrentTasks)

	logger.Printf("%d個のタスクを起動します...", len(cfg.Tasks))

	for _, task := range cfg.Tasks {
		select {
		case <-ctx.Done():
			log.Println("シャットダウンシグナルにより、新規タスクの開始を中止しました。")
			goto end_loop
		default:
		}

		wg.Add(1)
		taskSemaphore <- struct{}{}

		taskCopy := task

		go func() {
			defer func() { <-taskSemaphore }()
			defer wg.Done() // wg.Done()はGoroutineの最後に呼ばれるべき

			// コピーした変数 `taskCopy` を使う
			core.ExecuteTask(ctx, taskCopy, cfg.Network, cfg.SafetyStopMinDiskGB, isWatch)
		}()
	}
end_loop:
	wg.Wait()
	logger.Println("全てのCLIタスクが完了しました。")
}

// runVerificationModeは、検証モードの実行ロジックを担当します。（現在はスタブ）
func runVerificationMode(_ context.Context, _ string, _ string, _ bool, _ bool) {
	log.Println("検証モードは現在実装されていません。")
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
