// Package systray は、システムトレイアプリケーションのUIとイベントハンドリングを提供します。
package systray

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"GoImageBoardArchiver/internal/config"
	"GoImageBoardArchiver/internal/core"
	"GoImageBoardArchiver/internal/systray/icon"

	"fyne.io/systray"
)

// UIEvent はUIで発生したイベントの種類を表します。
type UIEvent int

const (
	ClickToggleWatch UIEvent = iota
	ClickRunOnce
	ClickPauseResume
	ClickOpenRootDir
	ClickOpenConfig
	ClickOpenLogs
	ClickExit
)

// AppStatus はコアエンジンからUIへ渡されるアプリケーションの状態を表します。
type AppStatus = core.AppStatus

// min は2つの整数の最小値を返します。
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// saveIconForDebug は、デバッグ用にアイコンデータを一時ファイルとして保存します。
func saveIconForDebug(data []byte, stateName string) (string, error) {
	tmpDir := filepath.Join(".", "debug_icons")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("デバッグディレクトリの作成に失敗: %w", err)
	}

	filename := filepath.Join(tmpDir, fmt.Sprintf("icon_%s.png", stateName))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return "", fmt.Errorf("アイコンファイルの書き込みに失敗: %w", err)
	}

	return filename, nil
}

// パッケージレベル変数
var (
	// --- チャネル ---
	uiEventChannel      chan UIEvent
	coreCommandChannel  chan string
	statusUpdateChannel chan AppStatus

	// --- メニュー項目 ---
	mStatusState   *systray.MenuItem
	mStatusDetail  *systray.MenuItem
	mStatusSession *systray.MenuItem
	mToggleWatch   *systray.MenuItem
	mRunOnce       *systray.MenuItem
	mPauseResume   *systray.MenuItem
	mOpenRootDir   *systray.MenuItem
	mOpenConfig    *systray.MenuItem
	mOpenLogs      *systray.MenuItem
	mExit          *systray.MenuItem

	// --- ライフサイクル管理 ---
	appCtx    context.Context
	appCancel context.CancelFunc
	coreWg    sync.WaitGroup
)

// RunSystrayApp は、システムトレイアプリケーションを開始します。
func RunSystrayApp(globalCtx context.Context) {
	appCtx, appCancel = context.WithCancel(globalCtx)
	defer appCancel()
	systray.Run(onReady, onExit)
}

// onReadyは、UIの初期化とバックグラウンドプロセスの起動を行います。
func onReady() {
	log.Printf("INFO: システムトレイの準備ができました (OS=%s, ARCH=%s)", runtime.GOOS, runtime.GOARCH)
	log.Println("INFO: UIを構築します...")

	// --- アイコンとツールチップの初期設定 ---
	// 初期状態はアイドル（グレー●）
	log.Printf("INFO: 初期アイコンの設定を開始します - %s", icon.GetIconInfo("Idle"))
	iconData := icon.DataIdle
	if err := icon.ValidateIconData(iconData); err != nil {
		log.Printf("ERROR: 初期アイコンデータの検証に失敗しました: %v", err)
		log.Printf("DEBUG: アイコンデータの先頭16バイト: %v", iconData[:min(16, len(iconData))])
	} else {
		log.Printf("INFO: アイコンデータの検証成功 (size=%d bytes)", len(iconData))

		// デバッグ用: アイコンデータを一時ファイルとして保存
		if debugIconPath, err := saveIconForDebug(iconData, "idle"); err == nil {
			log.Printf("DEBUG: デバッグ用アイコンを保存しました: %s", debugIconPath)
		} else {
			log.Printf("WARNING: デバッグ用アイコンの保存に失敗しました: %v", err)
		}

		log.Printf("DEBUG: systray.SetIcon()を呼び出します...")
		systray.SetIcon(iconData)
		log.Printf("DEBUG: systray.SetIcon()の呼び出しが完了しました")
	}
	systray.SetTitle("GIBA")
	systray.SetTooltip("GIBA: 初期化中...")

	// 2. メニュー項目の構築
	mStatusState = systray.AddMenuItem("状態: 初期化中...", "現在のアプリケーションの状態")
	mStatusDetail = systray.AddMenuItem("詳細: -", "現在実行中のタスクの詳細")
	mStatusSession = systray.AddMenuItem("セッション: -", "今回の起動中の統計情報")
	mStatusState.Disable()
	mStatusDetail.Disable()
	mStatusSession.Disable()
	systray.AddSeparator()

	mToggleWatch = systray.AddMenuItem("監視モードを有効にする", "バックグラウンドでの自動実行を切り替えます")
	mRunOnce = systray.AddMenuItem("今すぐ全タスクを実行", "手動で一度だけ実行します")
	mPauseResume = systray.AddMenuItem("すべての活動を一時停止", "現在および将来のタスクを一時停止します")
	systray.AddSeparator()

	mOpenRootDir = systray.AddMenuItem("保存先フォルダを開く", "アーカイブが保存されているメインフォルダを開きます")
	mLogsAndConfig := systray.AddMenuItem("ログと設定", "")
	mOpenConfig = mLogsAndConfig.AddSubMenuItem("設定ファイルを開く", "config.jsonを編集します")
	mOpenLogs = mLogsAndConfig.AddSubMenuItem("最新ログを開く", "ログファイルを開きます")
	systray.AddSeparator()

	mExit = systray.AddMenuItem("GIBAを終了", "アプリケーションを安全に終了します")

	// 3. チャネルの初期化
	uiEventChannel = make(chan UIEvent)
	coreCommandChannel = make(chan string)
	statusUpdateChannel = make(chan AppStatus, 5)

	// 4. UIイベントハンドラの起動
	go func() {
		for {
			select {
			case <-mToggleWatch.ClickedCh:
				uiEventChannel <- ClickToggleWatch
			case <-mRunOnce.ClickedCh:
				uiEventChannel <- ClickRunOnce
			case <-mPauseResume.ClickedCh:
				uiEventChannel <- ClickPauseResume
			case <-mOpenRootDir.ClickedCh:
				uiEventChannel <- ClickOpenRootDir
			case <-mOpenConfig.ClickedCh:
				uiEventChannel <- ClickOpenConfig
			case <-mOpenLogs.ClickedCh:
				uiEventChannel <- ClickOpenLogs
			case <-mExit.ClickedCh:
				uiEventChannel <- ClickExit
			}
		}
	}()

	// 5. UI更新ループの起動
	go startUIUpdateLoop()

	// 6. コアエンジンの起動
	coreWg.Add(1)
	go startCoreEngine(appCtx, coreCommandChannel, statusUpdateChannel, &coreWg)

	log.Println("UIの構築とバックグラウンドエンジンの起動が完了しました。")
}

// onExitは、アプリケーションが終了するときに呼び出されます。
func onExit() {
	log.Println("終了処理を開始します。")
	appCancel()
	coreWg.Wait()
	log.Println("全てのバックグラウンド処理が完了しました。アプリケーションを終了します。")
}

// startUIUpdateLoopは、UIの表示を管理するためのメインループです。
func startUIUpdateLoop() {
	log.Println("UI更新ループを開始しました。")
	for {
		select {
		case event := <-uiEventChannel:
			switch event {
			case ClickExit:
				log.Println("UI: 終了イベント受信。")
				systray.Quit()
				return
			case ClickToggleWatch:
				log.Println("UI: 監視モード切り替えイベント受信。")
				coreCommandChannel <- "toggle_watch"
			case ClickRunOnce:
				log.Println("UI: 手動実行イベント受信。")
				coreCommandChannel <- "run_once"
			case ClickPauseResume:
				log.Println("UI: 一時停止/再開イベント受信。")
				coreCommandChannel <- "toggle_pause"
			case ClickOpenConfig:
				log.Println("UI: 設定ファイルを開くイベント受信。")
				openCommand("config.json")
			case ClickOpenRootDir:
				log.Println("UI: ルートフォルダを開くイベント受信。")
				openCommand(".")
			case ClickOpenLogs:
				log.Println("UI: ログファイルを開くイベント受信。")
				openCommand("giba.log")
			}
		case status := <-statusUpdateChannel:
			stateStr := status.State.String()

			// 状態に応じてアイコンを切り替え
			log.Printf("DEBUG: アイコン更新要求 - %s", icon.GetIconInfo(stateStr))
			iconData := icon.GetIconForState(stateStr)
			if err := icon.ValidateIconData(iconData); err != nil {
				log.Printf("ERROR: アイコンデータの検証に失敗しました (state=%s): %v", stateStr, err)
				if len(iconData) > 0 {
					log.Printf("DEBUG: アイコンデータの先頭16バイト: %v", iconData[:min(16, len(iconData))])
				}
			} else {
				log.Printf("DEBUG: systray.SetIcon()を呼び出します (state=%s, size=%d bytes)", stateStr, len(iconData))
				systray.SetIcon(iconData)
				log.Printf("DEBUG: systray.SetIcon()の呼び出しが完了しました (state=%s)", stateStr)
			}

			systray.SetTooltip(fmt.Sprintf("GIBA: %s", stateStr))
			mStatusState.SetTitle(fmt.Sprintf("状態: %s", stateStr))
			mStatusDetail.SetTitle(fmt.Sprintf("詳細: %s", status.Detail))
			mStatusSession.SetTitle(fmt.Sprintf("セッション: %s", status.SessionInfo))

			if status.IsWatching {
				mToggleWatch.Check()
			} else {
				mToggleWatch.Uncheck()
			}

			isActuallyRunning := status.State == core.StateRunning || status.State == core.StatePreparing
			if isActuallyRunning {
				mRunOnce.Disable()
			} else {
				mRunOnce.Enable()
			}

			isPaused := status.IsPaused
			if isPaused {
				mPauseResume.SetTitle("活動を再開する")
			} else {
				mPauseResume.SetTitle("すべての活動を一時停止")
			}

		case <-appCtx.Done():
			log.Println("UI更新ループが終了シグナルを受信しました。")
			return
		}
	}
}

// openCommandはOSのデフォルトアプリケーションでファイルやフォルダを開きます。
func openCommand(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("コマンドの実行に失敗しました: %v", err)
	}
}

// startCoreEngineは、コアエンジンを起動するためのスタブ関数です。
func startCoreEngine(ctx context.Context, commandCh <-chan string, statusCh chan<- AppStatus, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println("コアエンジン(スタブ)が起動しました。")

	cfg, err := config.LoadAndResolve("config.json")
	if err != nil {
		log.Printf("FATAL: 設定ファイルの読み込みに失敗しました: %v", err)
		statusCh <- AppStatus{State: core.StateError, Detail: fmt.Sprintf("設定エラー: %v", err), HasError: true, ConfigLoaded: false}
		return
	}
	log.Printf("設定ファイル(v%s)を正常に読み込みました。", cfg.ConfigVersion)

	isWatching := false
	isRunning := false
	isPaused := false

	statusCh <- AppStatus{State: core.StateIdle, Detail: "待機中", IsWatching: isWatching, IsRunning: isRunning, IsPaused: isPaused, HasError: false, ConfigLoaded: true}

	tasks := cfg.Tasks
	if len(tasks) == 0 {
		log.Println("設定にタスクが見つかりませんでした。")
		statusCh <- AppStatus{State: core.StateIdle, Detail: "タスクなし", IsWatching: isWatching, IsRunning: isRunning, IsPaused: isPaused, HasError: false, ConfigLoaded: true}
	}

	// 監視モード用のタスク管理
	var watchTaskCancel context.CancelFunc
	var watchTaskWg sync.WaitGroup

	for {
		select {
		case cmd := <-commandCh:
			log.Printf("コアエンジン(スタブ): コマンド '%s' を受信しました。", cmd)
			switch cmd {
			case "toggle_watch":
				isWatching = !isWatching
				if isWatching {
					// 監視モードを開始
					log.Println("監視モードを開始します...")
					statusCh <- AppStatus{State: core.StateWatching, Detail: "監視モード有効", IsWatching: isWatching, IsRunning: isRunning, IsPaused: isPaused, HasError: false, ConfigLoaded: true}

					// 既存の監視タスクがあればキャンセル
					if watchTaskCancel != nil {
						watchTaskCancel()
						watchTaskWg.Wait()
					}

					// 新しい監視タスクを起動
					watchCtx, cancel := context.WithCancel(ctx)
					watchTaskCancel = cancel

					for _, task := range tasks {
						watchTaskWg.Add(1)
						go func(t config.Task) {
							defer watchTaskWg.Done()
							core.ExecuteTask(watchCtx, t, cfg.Network, cfg.SafetyStopMinDiskGB, true)
						}(task)
					}
				} else {
					// 監視モードを停止
					log.Println("監視モードを停止します...")
					if watchTaskCancel != nil {
						watchTaskCancel()
						watchTaskWg.Wait()
						watchTaskCancel = nil
					}
					statusCh <- AppStatus{State: core.StateIdle, Detail: "監視モード無効", IsWatching: isWatching, IsRunning: isRunning, IsPaused: isPaused, HasError: false, ConfigLoaded: true}
				}
			case "run_once":
				if !isRunning && !isWatching {
					go func() {
						isRunning = true
						statusCh <- AppStatus{State: core.StateRunning, Detail: "手動実行中...", IsWatching: isWatching, IsRunning: isRunning, IsPaused: isPaused, HasError: false, ConfigLoaded: true}

						var runOnceWg sync.WaitGroup
						for _, task := range tasks {
							runOnceWg.Add(1)
							go func(t config.Task) {
								defer runOnceWg.Done()
								core.ExecuteTask(ctx, t, cfg.Network, cfg.SafetyStopMinDiskGB, false)
							}(task)
						}
						runOnceWg.Wait()

						isRunning = false
						statusCh <- AppStatus{State: core.StateIdle, Detail: "手動実行完了", IsWatching: isWatching, IsRunning: isRunning, IsPaused: isPaused, HasError: false, ConfigLoaded: true}
					}()
				}
			case "toggle_pause":
				isPaused = !isPaused
				if isPaused {
					statusCh <- AppStatus{State: core.StatePaused, Detail: "全活動を一時停止しました", IsWatching: isWatching, IsRunning: isRunning, IsPaused: isPaused, HasError: false, ConfigLoaded: true}
				} else {
					statusCh <- AppStatus{State: core.StateIdle, Detail: "活動を再開しました", IsWatching: isWatching, IsRunning: isRunning, IsPaused: isPaused, HasError: false, ConfigLoaded: true}
				}
			}
		case <-ctx.Done():
			log.Println("コアエンジン(スタブ)が終了シグナルを受信し、シャットダウンします。")
			return
		}
	}
}
