package systray

// 司令塔ループ (coreEngine) の状態一元化を検証するテスト。
//
// - 実行 goroutine は注入した startTask に置き換え、core.ExecuteTask は呼ばない。
// - reload_config も注入した loadConfig を使うため config.json に依存しない。
// - `go test -race ./internal/systray` で、コマンド連打時にデータ競合が無いことを確認する。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"GoImageBoardArchiver/internal/config"
)

func testEngineConfig() *config.Config {
	enabled := true
	return &config.Config{
		ConfigVersion: "1.0",
		Tasks: []config.Task{
			{Enabled: &enabled, TaskName: "task-1"},
			{Enabled: &enabled, TaskName: "task-2"},
		},
	}
}

type engineHarness struct {
	engine    *coreEngine
	commandCh chan string
	statusCh  chan AppStatus
	runDoneCh chan runFinished
	uiCh      chan uiControl
	done      chan struct{}

	watchStarted  int64
	watchFinished int64
	manualStarted int64
	manualDone    int64

	// hot-reload の意味論検証用: startTask に渡されたタスク名を記録する。
	mu         sync.Mutex
	watchTask  []string
	manualTask []string
}

// recordTask は startTask に渡されたタスク名を記録します。
func (h *engineHarness) recordTask(name string, isWatch bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if isWatch {
		h.watchTask = append(h.watchTask, name)
	} else {
		h.manualTask = append(h.manualTask, name)
	}
}

// tasksOf は記録済みのタスク名を返します。
func (h *engineHarness) tasksOf(isWatch bool) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if isWatch {
		return append([]string(nil), h.watchTask...)
	}
	return append([]string(nil), h.manualTask...)
}

func newEngineHarness(ctx context.Context) *engineHarness {
	h := &engineHarness{
		commandCh: make(chan string),
		// ドレインしないテストでもループが止まらないよう大きめに取る。
		statusCh:  make(chan AppStatus, 512),
		runDoneCh: make(chan runFinished, 128),
		uiCh:      make(chan uiControl, 16),
		done:      make(chan struct{}),
	}

	e := &coreEngine{
		ctx:        ctx,
		commandCh:  h.commandCh,
		statusCh:   h.statusCh,
		runDoneCh:  h.runDoneCh,
		uiControl:  h.uiCh,
		loadConfig: func() (*config.Config, error) { return testEngineConfig(), nil },
	}
	e.startTask = func(runCtx context.Context, task config.Task, snap runSnapshot, isWatch bool, statusCh chan<- AppStatus) {
		h.recordTask(task.TaskName, isWatch)
		if isWatch {
			atomic.AddInt64(&h.watchStarted, 1)
			defer atomic.AddInt64(&h.watchFinished, 1)
			<-runCtx.Done()
			return
		}
		atomic.AddInt64(&h.manualStarted, 1)
		defer atomic.AddInt64(&h.manualDone, 1)
		select {
		case <-time.After(2 * time.Millisecond):
		case <-runCtx.Done():
		}
	}
	h.engine = e
	return h
}

func (h *engineHarness) run(ctx context.Context) {
	go func() {
		h.engine.run()
		close(h.done)
	}()
}

func (h *engineHarness) send(ctx context.Context, t *testing.T, cmd string) {
	t.Helper()
	select {
	case h.commandCh <- cmd:
	case <-time.After(2 * time.Second):
		t.Fatalf("コマンド %q を送れませんでした (ループが応答していない)", cmd)
	case <-ctx.Done():
		t.Fatalf("コマンド %q の送信前に ctx が終了しました", cmd)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("タイムアウト: %s", what)
}

// TestCoreEngineWatchAndManualRun は、監視開始→手動実行→監視再開の順序を検証します。
func TestCoreEngineWatchAndManualRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newEngineHarness(ctx)
	h.run(ctx)

	h.send(ctx, t, "toggle_watch")
	waitUntil(t, "監視タスク2件が開始される", func() bool { return atomic.LoadInt64(&h.watchStarted) == 2 })

	// 手動実行: 監視タスクの停止完了を待ってから開始される (重複しない)。
	h.send(ctx, t, "run_once")
	waitUntil(t, "手動実行が開始される", func() bool { return atomic.LoadInt64(&h.manualStarted) == 2 })
	waitUntil(t, "手動実行が完了する", func() bool { return atomic.LoadInt64(&h.manualDone) == 2 })
	waitUntil(t, "監視タスクが停止する", func() bool { return atomic.LoadInt64(&h.watchFinished) == 2 })
	waitUntil(t, "監視が再開される", func() bool {
		return atomic.LoadInt64(&h.watchStarted) == 4
	})

	// 監視 OFF → 監視タスクがすべて終了する。
	h.send(ctx, t, "toggle_watch")
	waitUntil(t, "監視タスクがすべて終了する", func() bool {
		return atomic.LoadInt64(&h.watchFinished) == atomic.LoadInt64(&h.watchStarted)
	})

	cancel()
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		t.Fatal("エンジンが終了しませんでした")
	}
}

// TestCoreEngineReloadRestartsWatch は、設定リロードで監視が新設定で再開されることを検証します。
func TestCoreEngineReloadRestartsWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newEngineHarness(ctx)
	h.run(ctx)

	h.send(ctx, t, "toggle_watch")
	waitUntil(t, "監視開始", func() bool { return atomic.LoadInt64(&h.watchStarted) == 2 })

	h.send(ctx, t, "reload_config")
	waitUntil(t, "旧監視の終了", func() bool { return atomic.LoadInt64(&h.watchFinished) == 2 })
	waitUntil(t, "新設定で監視再開", func() bool { return atomic.LoadInt64(&h.watchStarted) == 4 })

	// 手動実行中のリロードでも、手動実行は開始時スナップショットで完走する (REQ-3.3)。
	h.send(ctx, t, "run_once")
	waitUntil(t, "手動実行開始", func() bool { return atomic.LoadInt64(&h.manualStarted) == 2 })
	h.send(ctx, t, "reload_config")
	waitUntil(t, "手動実行は完走する", func() bool { return atomic.LoadInt64(&h.manualDone) == 2 })

	h.send(ctx, t, "toggle_watch")
	cancel()
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		t.Fatal("エンジンが終了しませんでした")
	}
}

// TestCoreEngineCommandStorm は、監視切替・手動実行・リロード・一時停止を同時に連打し、
// -race でデータ競合が報告されないことを確認します (§10.3 の再現操作に相当)。
func TestCoreEngineCommandStorm(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newEngineHarness(ctx)
	h.run(ctx)

	// ステータスは別 goroutine で消費し続ける (UI ループの代役)。
	drainCtx, stopDrain := context.WithCancel(ctx)
	defer stopDrain()
	go func() {
		for {
			select {
			case <-h.statusCh:
			case <-drainCtx.Done():
				return
			}
		}
	}()

	commands := []string{"toggle_watch", "run_once", "reload_config", "toggle_pause", "toggle_watch"}
	senders := 4
	iterations := 25
	errCh := make(chan error, senders)
	for s := 0; s < senders; s++ {
		go func(seed int) {
			for i := 0; i < iterations; i++ {
				cmd := commands[(seed+i)%len(commands)]
				select {
				case h.commandCh <- cmd:
				case <-time.After(2 * time.Second):
					errCh <- errSendTimeout
					return
				case <-ctx.Done():
					return
				}
			}
			errCh <- nil
		}(s)
	}
	for s := 0; s < senders; s++ {
		if err := <-errCh; err != nil {
			t.Fatalf("コマンド送信がタイムアウトしました: %v", err)
		}
	}

	// 終了状態を監視 OFF に寄せてから停止する。
	h.send(ctx, t, "toggle_watch")
	cancel()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("エンジンが終了しませんでした")
	}
}

var errSendTimeout = &sendTimeoutError{}

type sendTimeoutError struct{}

func (*sendTimeoutError) Error() string { return "send timeout" }

// TestCoreEngineHotReloadSemantics は、REQ-3.3 の意味論を検証します。
//   - 実行中のタスクは開始時点のスナップショットで最後まで動く
//   - 新設定は次回実行から効く
func TestCoreEngineHotReloadSemantics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := newEngineHarness(ctx)
	// リロード前後でタスク名が変わる設定を返す。
	var generation int32 = 1
	h.engine.loadConfig = func() (*config.Config, error) {
		g := atomic.LoadInt32(&generation)
		enabled := true
		name := "task-v1"
		if g >= 2 {
			name = "task-v2"
		}
		return &config.Config{
			ConfigVersion: "1.0",
			Tasks:         []config.Task{{Enabled: &enabled, TaskName: name}},
		}, nil
	}
	h.run(ctx)

	h.send(ctx, t, "toggle_watch")
	waitUntil(t, "v1 で監視開始", func() bool { return len(h.tasksOf(true)) == 1 })

	// 監視中に手動実行を開始し、その最中に設定をリロードする。
	h.send(ctx, t, "run_once")
	waitUntil(t, "手動実行 (v1) 開始", func() bool { return len(h.tasksOf(false)) == 1 })

	atomic.StoreInt32(&generation, 2)
	h.send(ctx, t, "reload_config")

	// 実行中の手動実行は v1 のまま完走する。
	waitUntil(t, "手動実行が v1 のまま完走", func() bool { return atomic.LoadInt64(&h.manualDone) == 1 })
	if got := h.tasksOf(false); len(got) != 1 || got[0] != "task-v1" {
		t.Fatalf("手動実行のタスク名: got %v, want [task-v1] (開始時スナップショット)", got)
	}

	// リロード後、監視は v2 で再開する (次回実行から新設定)。
	waitUntil(t, "v2 で監視再開", func() bool {
		tasks := h.tasksOf(true)
		return len(tasks) >= 2 && tasks[len(tasks)-1] == "task-v2"
	})

	h.send(ctx, t, "toggle_watch")
	cancel()
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		t.Fatal("エンジンが終了しませんでした")
	}
}
