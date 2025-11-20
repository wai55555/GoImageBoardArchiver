// Package systray は、システムトレイアプリケーションのUIとイベントハンドリングを提供します。
package systray

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

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
	mWatchStatus   *systray.MenuItem
	mToggleWatch   *systray.MenuItem
	mRunOnce       *systray.MenuItem
	mPauseResume   *systray.MenuItem
	mConsoleToggle *systray.MenuItem
	mLogFileToggle *systray.MenuItem
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
func RunSystrayApp(globalCtx context.Context, showConsoleFunc, hideConsoleFunc func(), toggleLoggerFunc func(bool, string) error) {
	appCtx, appCancel = context.WithCancel(globalCtx)
	defer appCancel()

	// コールバック関数を保持
	showConsole = showConsoleFunc
	hideConsole = hideConsoleFunc
	toggleLogger = toggleLoggerFunc

	systray.Run(onReady, onExit)
}

// コールバック関数保持用変数
var (
	showConsole  func()
	hideConsole  func()
	toggleLogger func(bool, string) error
)

// onReadyは、UIの初期化とバックグラウンドプロセスの起動を行います。
func onReady() {
	log.Printf("INFO: システムトレイの準備ができました (OS=%s, ARCH=%s)", runtime.GOOS, runtime.GOARCH)
	log.Println("INFO: UIを構築します...")

	// --- アイコンとツールチップの初期設定 ---
	iconData := icon.GetIconData("Idle")
	if err := icon.ValidateIconData(iconData); err == nil {
		systray.SetIcon(iconData)
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

	// 監視ステータス（カウントダウン用）
	mWatchStatus = systray.AddMenuItem("待機中: -", "次の実行までの時間")
	mWatchStatus.Disable() // 情報表示用なので無効化

	mToggleWatch = systray.AddMenuItem("監視モードを有効にする", "バックグラウンドでの自動実行を切り替えます")
	mRunOnce = systray.AddMenuItem("今すぐ全タスクを実行", "手動で一度だけ実行します")
	mPauseResume = systray.AddMenuItem("すべての活動を一時停止", "現在および将来のタスクを一時停止します")
	systray.AddSeparator()

	// コンソール・ログ制御
	mConsoleToggle = systray.AddMenuItemCheckbox("コンソールを表示", "コンソールウィンドウの表示/非表示を切り替えます", false)
	mLogFileToggle = systray.AddMenuItemCheckbox("ログファイルに出力", "ログをファイル(giba.log)にも出力します", false)
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
	statusUpdateChannel = make(chan AppStatus, 10)

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
			case <-mConsoleToggle.ClickedCh:
				if mConsoleToggle.Checked() {
					mConsoleToggle.Uncheck()
					hideConsole()
				} else {
					mConsoleToggle.Check()
					showConsole()
				}
			case <-mLogFileToggle.ClickedCh:
				if mLogFileToggle.Checked() {
					mLogFileToggle.Uncheck()
					toggleLogger(false, "")
				} else {
					mLogFileToggle.Check()
					toggleLogger(true, "")
				}
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
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var nextRunTime time.Time
	var isWatching bool
	var isRunning bool
	var animationFrame int

	for {
		select {
		case <-ticker.C:
			// 1秒ごとの更新処理
			if isRunning {
				// 実行中アニメーション
				dots := ""
				switch animationFrame % 4 {
				case 0:
					dots = ""
				case 1:
					dots = "."
				case 2:
					dots = ".."
				case 3:
					dots = "..."
				}
				mWatchStatus.SetTitle(fmt.Sprintf("実行中%s", dots))
				animationFrame++
			} else if isWatching && !nextRunTime.IsZero() {
				// カウントダウン
				remaining := time.Until(nextRunTime)
				if remaining > 0 {
					mWatchStatus.SetTitle(fmt.Sprintf("待機中 (残 %02d:%02d)", int(remaining.Minutes()), int(remaining.Seconds())%60))
				} else {
					mWatchStatus.SetTitle("実行準備中...")
				}
			} else if isWatching {
				mWatchStatus.SetTitle("待機中")
			} else {
				mWatchStatus.SetTitle("停止中")
			}

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
				today := time.Now().Format("2006-01-02")
				logFileName := fmt.Sprintf("giba_%s.log", today)
				openCommand(logFileName)
			}
		case status := <-statusUpdateChannel:
			stateStr := status.State.String()
			isWatching = status.IsWatching
			isRunning = status.State == core.StateRunning || status.State == core.StatePreparing

			// NEXT_RUN情報の解析 (Detailフィールドに含まれると仮定: "NEXT_RUN:1234567890")
			if len(status.Detail) > 9 && status.Detail[:9] == "NEXT_RUN:" {
				tsStr := status.Detail[9:]
				if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
					nextRunTime = time.Unix(ts, 0)
				}
			}

			// アイコン更新
			iconData := icon.GetIconData(stateStr)
			if err := icon.ValidateIconData(iconData); err == nil {
				systray.SetIcon(iconData)
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

			if isRunning {
				mRunOnce.Disable()
			} else {
				mRunOnce.Enable()
			}

			if status.IsPaused {
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

	// 初期ログ設定の反映
	if cfg.EnableLogFile {
		// UIスレッド経由ではないため直接呼び出すと競合の可能性があるが、初期化時なので許容
		// ただし、mLogFileToggleの状態も更新する必要があるため、UIイベントとして処理するのが理想
		// ここでは簡易的にトグル関数を呼ぶ
		if toggleLogger != nil {
			toggleLogger(true, cfg.LogFilePath)
			// 注意: mLogFileToggle.Check() はメインスレッド以外から呼ぶと安全でない可能性があるため、ここでは行わない
			// 必要ならUIイベントを送る
		}
	}

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
							core.ExecuteTask(watchCtx, t, cfg.Network, cfg.SafetyStopMinDiskGB, true, statusCh)
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
								core.ExecuteTask(ctx, t, cfg.Network, cfg.SafetyStopMinDiskGB, false, statusCh)
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
