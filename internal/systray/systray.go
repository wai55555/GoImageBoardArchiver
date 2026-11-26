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
	"GoImageBoardArchiver/internal/webui"

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

// パッケージレベル変数
var (
	// --- チャネル ---
	uiEventChannel      chan UIEvent
	coreCommandChannel  chan string
	statusUpdateChannel chan AppStatus
	runFinishedChannel  chan runFinished
	uiControlChannel    chan uiControl

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
	mOpenConfig = mLogsAndConfig.AddSubMenuItem("設定画面を開く", "Web UIで設定を編集します")
	mOpenLogs = mLogsAndConfig.AddSubMenuItem("最新ログを開く", "ログファイルを開きます")
	systray.AddSeparator()

	mExit = systray.AddMenuItem("GIBAを終了", "アプリケーションを安全に終了します")

	// 3. チャネルの初期化
	uiEventChannel = make(chan UIEvent)
	coreCommandChannel = make(chan string)
	statusUpdateChannel = make(chan AppStatus, 10)
	// 実行 goroutine の完了通知。状態を書き換えるのは司令塔ループだけなので、
	// goroutine からはこのチャネルへ「終わった」ことだけを返す。
	runFinishedChannel = make(chan runFinished, 32)
	uiControlChannel = make(chan uiControl)

	// Web UIでconfig.jsonが保存されたら、コアエンジンに再読み込みを指示する。
	// （config.jsonは起動時に一度しか読み込まれないため、これがないとWeb UIでの
	//   保存内容がアプリ再起動まで一切反映されない）
	webui.OnConfigSaved = func() {
		select {
		case coreCommandChannel <- "reload_config":
		case <-appCtx.Done():
		}
	}

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
					if err := toggleLogger(false, ""); err != nil {
						log.Printf("ERROR: ログファイル出力の無効化に失敗しました: %v", err)
					} else {
						log.Println("ログファイル出力を無効にしました")
					}
				} else {
					mLogFileToggle.Check()
					// 空文字列を渡すことで、デフォルトの日付形式ファイル名が使われる
					if err := toggleLogger(true, ""); err != nil {
						log.Printf("ERROR: ログファイル出力の有効化に失敗しました: %v", err)
						mLogFileToggle.Uncheck()
					} else {
						log.Println("ログファイル出力を有効にしました")
					}
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
	go startCoreEngine(appCtx, coreCommandChannel, statusUpdateChannel, runFinishedChannel, uiControlChannel, &coreWg)

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
	var animationFrame int

	// 各タスクの状態を追跡するためのマップ
	taskStates := make(map[string]core.AppState)

	// セッション統計情報（UI側で集計）
	sessionStats := &core.SessionStats{
		StartTime: time.Now(),
	}

	for {
		// --- 全体の実行状態を判定 ---
		var isAnyTaskRunning bool
		var runningTaskCount int
		for _, state := range taskStates {
			if state == core.StateRunning || state == core.StatePreparing {
				isAnyTaskRunning = true
				runningTaskCount++
			}
		}

		select {
		case <-ticker.C:
			// 1秒ごとの更新処理
			if isAnyTaskRunning {
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
				mWatchStatus.SetTitle(fmt.Sprintf("実行中 (%dタスク)%s", runningTaskCount, dots))
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

		case ctrl := <-uiControlChannel:
			// コアエンジンからの UI 操作依頼 (メニューは UI ループだけが触る)。
			switch ctrl {
			case uiControlCheckLogFileToggle:
				mLogFileToggle.Check()
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
				log.Println("UI: 設定Web UIを開くイベント受信。")
				webui.StartWebServer()
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
			// --- 状態の更新 ---
			if status.TaskName != "" {
				taskStates[status.TaskName] = status.State
			}
			isWatching = status.IsWatching

			// 統計情報の更新（増分加算）
			if status.Stats != nil {
				sessionStats.ThreadsArchived += status.Stats.ThreadsArchived
				sessionStats.FilesDownloaded += status.Stats.FilesDownloaded
				sessionStats.TotalBytesWritten += status.Stats.TotalBytesWritten
			}

			// NEXT_RUN情報の解析 (Detailフィールドに含まれると仮定: "NEXT_RUN:1234567890")
			if len(status.Detail) > 9 && status.Detail[:9] == "NEXT_RUN:" {
				tsStr := status.Detail[9:]
				if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
					nextRunTime = time.Unix(ts, 0)
				}
			}

			// --- UIの更新 ---
			// isAnyTaskRunning はループの先頭で再計算される
			var iconState string
			if isAnyTaskRunning {
				iconState = "実行中"
			} else if isWatching {
				iconState = "監視中"
			} else {
				iconState = status.State.String()
			}

			iconData := icon.GetIconData(iconState)
			if err := icon.ValidateIconData(iconData); err == nil {
				systray.SetIcon(iconData)
			}

			systray.SetTooltip(fmt.Sprintf("GIBA: %s", iconState))
			mStatusState.SetTitle(fmt.Sprintf("状態: %s", iconState))
			mStatusDetail.SetTitle(fmt.Sprintf("詳細: %s", status.Detail))
			mStatusSession.SetTitle(fmt.Sprintf("セッション: %s", sessionStats.FormatSessionInfo()))

			if status.IsWatching {
				mToggleWatch.Check()
			} else {
				mToggleWatch.Uncheck()
			}

			if isAnyTaskRunning {
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

// --- コアエンジン (司令塔ループ) -------------------------------------------
//
// 設計方針 (REQ-3.2):
//   - isWatching / isPaused / 設定スナップショット / cancel 関数の所有者は、このループだけ。
//   - コマンドはチャネルでループに届き、状態変更はループ内でのみ行う。
//   - 実行用 goroutine は「開始時点のスナップショット」と context だけを受け取り、
//     完了を runFinishedChannel へ返す。共有変数は読み書きしない。
//   - ループは実行完了を待たない (待つと監視切替・中止・リロードが処理できなくなる)。
//     停止→再開の順序が必要な箇所は、完了通知を数えることで非同期に守る。

// runSnapshot は実行開始時点で固定する設定のスナップショットです。
type runSnapshot struct {
	cfg   *config.Config
	tasks []config.Task
}

// runKind は実行の種類です。
type runKind int

const (
	runKindWatch runKind = iota
	runKindManual
)

// runFinished は実行 goroutine の完了通知です。
type runFinished struct {
	kind runKind
	gen  uint64
}

// uiControl はコアエンジンから UI スレッドへ依頼する UI 操作です。
type uiControl int

const (
	uiControlCheckLogFileToggle uiControl = iota
)

// taskStarter は1タスク分の実行を開始します (テストで差し替え可能)。
type taskStarter func(ctx context.Context, task config.Task, snap runSnapshot, isWatch bool, statusCh chan<- AppStatus)

// defaultTaskStarter は core.ExecuteTask を呼ぶ既定の実装です。
func defaultTaskStarter(ctx context.Context, task config.Task, snap runSnapshot, isWatch bool, statusCh chan<- AppStatus) {
	core.ExecuteTask(ctx, task, snap.cfg.Network, snap.cfg.SafetyStopMinDiskGB, isWatch, statusCh)
}

// coreEngine は司令塔ループの状態を保持します。
// この構造体のフィールドは run() を実行している goroutine だけが読み書きします。
type coreEngine struct {
	ctx       context.Context
	commandCh <-chan string
	statusCh  chan<- AppStatus
	runDoneCh chan runFinished
	uiControl chan<- uiControl

	loadConfig func() (*config.Config, error)
	startTask  taskStarter

	snap       runSnapshot
	isWatching bool
	isPaused   bool

	// 監視実行
	watchCancel  context.CancelFunc
	watchGen     uint64
	pendingWatch int
	// 監視の再開待ち (手動実行の前後や設定リロードで、旧監視の停止完了を待ってから再開する)
	restartWatchPending bool

	// 手動実行
	manualCancel  context.CancelFunc
	manualGen     uint64
	manualRunning bool
	pendingManual int
	// 監視タスクの停止完了を待ってから開始する手動実行要求
	manualRequested bool
}

// startCoreEngine は司令塔ループを起動します。
func startCoreEngine(ctx context.Context, commandCh <-chan string, statusCh chan<- AppStatus, runDoneCh chan runFinished, uiControlCh chan<- uiControl, wg *sync.WaitGroup) {
	defer wg.Done()

	e := &coreEngine{
		ctx:        ctx,
		commandCh:  commandCh,
		statusCh:   statusCh,
		runDoneCh:  runDoneCh,
		uiControl:  uiControlCh,
		loadConfig: func() (*config.Config, error) { return config.LoadAndResolve("config.json") },
		startTask:  defaultTaskStarter,
	}
	e.run()
}

// run は司令塔ループ本体です。
func (e *coreEngine) run() {
	log.Println("コアエンジン(スタブ)が起動しました。")

	cfg, err := e.loadConfig()
	if err != nil {
		log.Printf("FATAL: 設定ファイルの読み込みに失敗しました: %v", err)
		e.sendStatus(AppStatus{State: core.StateError, Detail: fmt.Sprintf("設定エラー: %v", err), HasError: true, ConfigLoaded: false})
		return
	}
	log.Printf("設定ファイル(v%s)を正常に読み込みました。", cfg.ConfigVersion)
	e.snap = runSnapshot{cfg: cfg, tasks: cfg.Tasks}

	// 初期ログ設定の反映は UI 側 (メニュー操作) に依頼する。ここからメニューを直接触らない。
	if cfg.EnableLogFile {
		e.requestUIControl(uiControlCheckLogFileToggle)
	}

	e.sendStatus(AppStatus{State: core.StateIdle, Detail: "待機中", ConfigLoaded: true})
	if len(e.snap.tasks) == 0 {
		log.Println("設定にタスクが見つかりませんでした。")
		e.sendStatus(AppStatus{State: core.StateIdle, Detail: "タスクなし", ConfigLoaded: true})
	}

	statsTicker := time.NewTicker(10 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-statsTicker.C:
			e.sendStatus(AppStatus{State: core.StateIdle, Detail: "統計更新"})

		case cmd := <-e.commandCh:
			e.handleCommand(cmd)

		case done := <-e.runDoneCh:
			e.handleRunFinished(done)

		case <-e.ctx.Done():
			log.Println("コアエンジン(スタブ)が終了シグナルを受信し、シャットダウンします。")
			e.shutdown()
			return
		}
	}
}

// sendStatus は現在の状態を付加して UI へ送ります。
func (e *coreEngine) sendStatus(s AppStatus) {
	s.IsWatching = e.isWatching
	s.IsPaused = e.isPaused
	select {
	case e.statusCh <- s:
	case <-e.ctx.Done():
	}
}

// requestUIControl は UI スレッドへ操作を依頼します。
// UI が混んでいて受け取れない場合は、表示上の更新を諦める (状態そのものではないため)。
func (e *coreEngine) requestUIControl(c uiControl) {
	select {
	case e.uiControl <- c:
	default:
		log.Println("WARNING: UI操作依頼を送れませんでした (UIが多忙)")
	}
}

// handleCommand はコマンドを処理します。状態の書き換えはすべてここ (ループ内) で行います。
func (e *coreEngine) handleCommand(cmd string) {
	log.Printf("コアエンジン(スタブ): コマンド '%s' を受信しました。", cmd)

	switch cmd {
	case "toggle_watch":
		e.isWatching = !e.isWatching
		if e.isWatching {
			log.Println("監視モードを開始します...")
			e.sendStatus(AppStatus{State: core.StateWatching, Detail: "監視モード有効"})
			e.restartWatchPending = false
			e.startWatch()
		} else {
			log.Println("監視モードを停止します...")
			// 停止後の再開はしない。実行中の監視タスクは cancel で終了し、
			// 完了通知は handleRunFinished で消費される。
			e.restartWatchPending = false
			e.stopWatch()
			e.sendStatus(AppStatus{State: core.StateIdle, Detail: "監視モード無効"})
		}

	case "run_once":
		e.requestManualRun()

	case "toggle_pause":
		e.isPaused = !e.isPaused
		if e.isPaused {
			e.sendStatus(AppStatus{State: core.StatePaused, Detail: "全活動を一時停止しました"})
		} else {
			e.sendStatus(AppStatus{State: core.StateIdle, Detail: "活動を再開しました"})
		}

	case "reload_config":
		e.reloadConfig()
	}
}

// requestManualRun は手動実行要求を受け付けます。
// 監視タスクが動いている場合は、停止完了を待ってから開始する (重複ダウンロードを避ける)。
func (e *coreEngine) requestManualRun() {
	if e.manualRunning || e.manualRequested {
		log.Println("手動実行は既に実行中です。要求を無視します。")
		e.sendStatus(AppStatus{State: core.StateRunning, Detail: "手動実行中..."})
		return
	}

	if e.isWatching {
		e.restartWatchPending = true
		e.stopWatch()
	}

	if e.pendingWatch > 0 {
		log.Println("監視タスクの停止を待って手動実行を開始します")
		e.manualRequested = true
		e.sendStatus(AppStatus{State: core.StatePreparing, Detail: "監視タスクの停止待ち..."})
		return
	}

	e.startManualRun()
}

// startManualRun は手動実行の goroutine 群を起動します。
func (e *coreEngine) startManualRun() {
	snap := e.snap
	tasks := enabledTasks(snap.tasks)

	e.sendStatus(AppStatus{State: core.StateRunning, Detail: "手動実行中..."})
	if len(tasks) == 0 {
		log.Println("有効なタスクがありません。手動実行を完了します。")
		e.completeManualRun()
		return
	}

	mctx, cancel := context.WithCancel(e.ctx)
	e.manualGen++
	gen := e.manualGen
	e.manualCancel = cancel
	e.manualRunning = true
	e.pendingManual = len(tasks)

	for _, task := range tasks {
		go func(t config.Task) {
			defer continuousRunDone(e, runKindManual, gen)
			e.startTask(mctx, t, snap, false, e.statusCh)
		}(task)
	}
}

// continuousRunDone は完了通知を返します。シャットダウン後は通知せずに終了します。
func continuousRunDone(e *coreEngine, kind runKind, gen uint64) {
	select {
	case e.runDoneCh <- runFinished{kind: kind, gen: gen}:
	case <-e.ctx.Done():
	}
}

// handleRunFinished は実行 goroutine の完了を処理します。
func (e *coreEngine) handleRunFinished(done runFinished) {
	switch done.kind {
	case runKindWatch:
		if e.pendingWatch > 0 {
			e.pendingWatch--
		}
		if e.pendingWatch > 0 {
			return
		}
		if e.manualRequested && !e.manualRunning {
			e.manualRequested = false
			e.startManualRun()
			return
		}
		e.maybeRestartWatch()

	case runKindManual:
		if done.gen != e.manualGen {
			// 停止・再起動前の古い完了通知は無視する。
			return
		}
		if e.pendingManual > 0 {
			e.pendingManual--
		}
		if e.pendingManual > 0 {
			return
		}
		e.completeManualRun()
	}
}

// completeManualRun は手動実行の完了処理を行います。
func (e *coreEngine) completeManualRun() {
	e.manualRunning = false
	if e.manualCancel != nil {
		e.manualCancel()
		e.manualCancel = nil
	}
	e.sendStatus(AppStatus{State: core.StateIdle, Detail: "手動実行完了"})
	e.maybeRestartWatch()
}

// startWatch は監視実行の goroutine 群を起動します。
// 呼び出しはループ内のみ。cancel の所有権もループが持ちます。
func (e *coreEngine) startWatch() {
	if !e.isWatching || e.watchCancel != nil {
		return
	}

	snap := e.snap
	tasks := enabledTasks(snap.tasks)
	if len(tasks) == 0 {
		log.Println("監視対象の有効タスクがありません。")
		return
	}

	wctx, cancel := context.WithCancel(e.ctx)
	e.watchGen++
	gen := e.watchGen
	e.watchCancel = cancel
	e.pendingWatch += len(tasks)

	for _, task := range tasks {
		go func(t config.Task) {
			defer continuousRunDone(e, runKindWatch, gen)
			e.startTask(wctx, t, snap, true, e.statusCh)
		}(task)
	}
}

// stopWatch は監視実行をキャンセルします。完了は待ちません (ループを止めないため)。
func (e *coreEngine) stopWatch() {
	if e.watchCancel != nil {
		e.watchCancel()
		e.watchCancel = nil
	}
}

// maybeRestartWatch は、停止完了後に監視を再開すべき場合に再開します。
func (e *coreEngine) maybeRestartWatch() {
	if !e.restartWatchPending {
		return
	}
	if !e.isWatching || e.manualRunning || e.manualRequested || e.pendingWatch > 0 {
		return
	}
	e.restartWatchPending = false
	e.startWatch()
}

// reloadConfig は config.json を再読み込みします。
// 実行中の手動実行は開始時スナップショットのまま継続し、新設定は次回実行から効きます (REQ-3.3)。
func (e *coreEngine) reloadConfig() {
	log.Println("Web UIからの設定保存を検知しました。config.jsonを再読み込みします...")

	newCfg, err := e.loadConfig()
	if err != nil {
		// AGENT.md「Defensive Config Access」: 壊れた設定を読み込んでも、
		// 実行中のスナップショットは書き換えず、今まで通りの設定で動作を継続する。
		log.Printf("ERROR: 設定の再読み込みに失敗しました。既存の設定のまま動作を継続します: %v", err)
		e.sendStatus(AppStatus{State: core.StateIdle, Detail: fmt.Sprintf("設定再読み込み失敗: %v", err), HasError: true, ConfigLoaded: true})
		return
	}

	e.snap = runSnapshot{cfg: newCfg, tasks: newCfg.Tasks}
	log.Printf("設定を再読み込みしました。タスク数: %d", len(newCfg.Tasks))

	if e.isWatching {
		// 旧設定で動いている監視タスクを止め、停止完了後に新設定で再開する。
		e.restartWatchPending = true
		e.stopWatch()
	}

	e.sendStatus(AppStatus{State: core.StateIdle, Detail: "設定を再読み込みしました", ConfigLoaded: true})
	e.maybeRestartWatch()
}

// shutdown は実行中の goroutine をキャンセルします。
func (e *coreEngine) shutdown() {
	e.restartWatchPending = false
	e.manualRequested = false
	e.stopWatch()
	if e.manualCancel != nil {
		e.manualCancel()
		e.manualCancel = nil
	}
}

// enabledTasks は有効なタスクだけを返します (スナップショットのコピー)。
func enabledTasks(tasks []config.Task) []config.Task {
	enabled := make([]config.Task, 0, len(tasks))
	for _, task := range tasks {
		if task.Enabled == nil || !*task.Enabled {
			continue
		}
		enabled = append(enabled, task)
	}
	return enabled
}
