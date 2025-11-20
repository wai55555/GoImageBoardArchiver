### **GIBA Webベース設定UI: 技術仕様書 (Ver 1.0 - 改訂版)**

**ステータス:** 提案
**作成者:** リード開発者
**日付:** 2023/10/27 (改訂)

---

### **1. 概要 (Executive Summary)**

本仕様書は、Go Image Board Archiver (GIBA) の設定 (`config.json`) を、GUIライブラリへの新たな依存を追加することなく、Webブラウザ経由で編集可能にするための技術的アプローチを定義する。

このアプローチは、アプリケーションの軽量性を維持しつつ、リッチでインタラクティブな設定体験を提供することを目的とする。中核となるのは、Goの標準ライブラリのみで構築された一時的な内部Webサーバーと、バイナリに埋め込まれたフロントエンドアセットである。

### **2. 設計思想 (Core Principles)**

1.  **依存関係の最小化 (Zero External Dependencies):** `fyne`, `qt`, `walk` といった外部GUIライブラリは一切導入しない。Go標準の `net/http` と `embed` パッケージを最大限に活用する。
2.  **パフォーマンス影響の極小化 (Minimal Performance Impact):** 設定UIはオンデマンドで起動・終了する。常駐はせず、設定時以外のCPU・メモリリソース消費はゼロを目指す。
3.  **セキュリティの確保 (Secure by Default):** 内部Webサーバーは、外部からのアクセスを原理的に不可能にするため、ローカルループバックアドレス (`127.0.0.1`) のみにバインドする。
4.  **単一バイナリの維持 (Single Binary Distribution):** HTML, CSS, JavaScriptといったフロントエンドアセットはすべてGoバイナリに埋め込み、配布の容易さを損なわない。
5.  **優れたUX (Rich User Experience):** Web技術の柔軟性を活かし、タスクの動的な追加・削除、入力値のリアルタイム検証など、ネイティブGUIに匹敵する、あるいはそれ以上のユーザー体験を提供する。

### **3. システムアーキテクチャ (System Architecture)**

#### 3.1. コンポーネント図

```
+---------------------------------+      +---------------------------------+
|      GIBA Core Application      |      |      User's Web Browser         |
|       (giba.exe)                |      | (Chrome, Firefox, etc.)         |
+---------------------------------+      +---------------------------------+
  Systray UI Thread              |<---->|  User Interaction               |
   - "設定を開く" Menu Item      |      |   - View/Edit Config Form       |
+---------------------------------+      |   - Click "Save" Button         |
                                 |      +---------------------------------+
  Internal Web Server (Go Routine)|             ^      |
   - `net/http` on 127.0.0.1:[PORT]|             |      | HTTP Request/Response
   - Serves Embedded Assets      |             |      v
   - Provides RESTful API        |      +---------------------------------+
     - GET /api/config           |<---->|  Frontend JavaScript (app.js)   |
     - POST /api/config          |      |   - Fetch data from API         |
+---------------------------------+      |   - Update DOM                  |
                           |      |   - Send data to API            |
 Read/Write                +---------------------------------+
           v
+---------------------------------+
|  File System                    |
|   - config.json                 |
+---------------------------------+
```

#### 3.2. 処理シーケンス

1.  **起動:** ユーザーがシステムトレイの「設定を開く」をクリックする。
2.  **サーバー始動:** GIBAアプリケーションは、バックグラウンドで新しいGoroutineを開始する。
    a.  空いているTCPポートを動的にスキャンして確保する（例: `8753`）。
    b.  `net/http`サーバーを `127.0.0.1:[PORT]` で起動する。
    c.  サーバーは、一定時間（例: 10分）リクエストがなければ自動でシャットダウンするタイムアウト機構を持つ。
3.  **ブラウザ起動:** GIBAは、OSのデフォルトブラウザで `http://127.0.0.1:[PORT]` を開くコマンドを実行する。
4.  **フロントエンド表示:**
    a.  ブラウザが `GET /` をリクエスト。サーバーは埋め込まれた `index.html` を返す。
    b.  HTML内のJavaScript (`app.js`) が実行され、`GET /api/config` をリクエスト。
    c.  サーバーは `config.json` を読み込み、その内容をJSON形式で返す。
    d.  JavaScriptは受け取ったJSONを元に、HTMLフォームの各入力フィールドを動的に構築・ заполнениеする。
5.  **ユーザー操作と保存:**
    a.  ユーザーがフォームを編集し、「保存」ボタンをクリックする。
    b.  JavaScriptはフォームの内容をシリアライズし、`config.json` と同じ構造のJSONオブジェクトを生成する。
    c.  生成したJSONをボディに含め、`POST /api/config` をリクエストする。
6.  **バックエンド処理:**
    a.  サーバーは `POST` リクエストを受け取る。
    b.  受け取ったJSONデータのバリデーション（型、必須項目など）を行う。
    c.  バリデーションが成功すれば、`config.json` ファイルを上書き保存する。
    d.  処理結果（成功またはエラーメッセージ）をJSONでブラウザに返す。
7.  **終了:**
    a.  JavaScriptは保存成功のメッセージをユーザーに表示する。
    b.  （推奨）成功後、JavaScriptは `POST /api/shutdown` をリクエストし、サーバーに自己終了を促す。
    c.  ユーザーはブラウザのタブを閉じる。サーバーはタイムアウトまたはシャットダウンAPIにより終了し、リソースを解放する。

### **4. 実装詳細と疑似コード (Implementation Details & Pseudocode)**

#### 4.1. バックエンド (Go)

##### **ディレクトリ構造:**

```
internal/
└── webui/
    ├── webui.go             // Webサーバーのロジック
    ├── embed/
    │   ├── index.html       // フロントエンドのメインHTML
    │   └── static/
    │       ├── style.css
    │       └── app.js
    └── templates/             // (オプション) HTMLテンプレート
```

##### **`internal/webui/webui.go` (疑似コード):**

```go
package webui

import (
    "context"
    "embed"
    "encoding/json"
    "fmt"
    "io/fs"
    "log"
    "net"
    "net/http"
    "os"
    "path/filepath" // パス操作用
    "time"

    "GoImageBoardArchiver/internal/config"
)

//go:embed embed/*
var embeddedAssets embed.FS

// serverContext はWebサーバーのインスタンスと状態を管理する
type serverContext struct {
    listener net.Listener
    server   *http.Server
    port     int
    // サーバー起動時のコンテキストを保持し、シャットダウン時に利用
    shutdownCtx    context.Context
    shutdownCancel context.CancelFunc
}

var currentServer *serverContext
var serverMutex sync.Mutex // サーバーインスタンスへの同時アクセスを保護

// StartWebServer はWebサーバーを非同期で起動し、ブラウザを開く
func StartWebServer() {
    serverMutex.Lock()
    defer serverMutex.Unlock()

    if currentServer != nil {
        log.Println("Web UIサーバーはすでに起動しています。既存のサーバーを利用します。")
        // すでに起動している場合はブラウザで開くだけ
        openBrowser(fmt.Sprintf("http://127.0.0.1:%d", currentServer.port))
        return
    }

    // 1. 空きポートを検索
    listener, err := net.Listen("tcp", "127.0.0.1:0") // ループバックアドレスに限定
    if err != nil {
        log.Printf("FATAL: Web UIサーバーの起動に失敗しました: 空きポートでのリッスンに失敗しました: %v", err)
        return
    }
    port := listener.Addr().(*net.TCPAddr).Port

    // 2. HTTPハンドラをセットアップ
    mux := http.NewServeMux()

    // 静的ファイル用のハンドラ (CSS, JS)
    staticFS, err := fs.Sub(embeddedAssets, "embed/static")
    if err != nil {
        log.Printf("FATAL: 埋め込み静的ファイルの読み込みに失敗しました: %v", err)
        listener.Close()
        return
    }
    mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

    // APIエンドポイント
    mux.HandleFunc("/api/config", handleConfig)
    mux.HandleFunc("/api/shutdown", handleShutdown)

    // ルートハンドラ (index.html)
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" {
            http.NotFound(w, r)
            return
        }
        rootFS, err := fs.Sub(embeddedAssets, "embed")
        if err != nil {
            log.Printf("ERROR: 埋め込みルートファイルの読み込みに失敗しました: %v", err)
            http.Error(w, "Internal Server Error", http.StatusInternalServerError)
            return
        }
        index, err := rootFS.ReadFile("index.html")
        if err != nil {
            log.Printf("ERROR: index.htmlの読み込みに失敗しました: %v", err)
            http.Error(w, "Internal Server Error", http.StatusInternalServerError)
            return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Write(index)
    })

    // 3. サーバーの定義とタイムアウト設定
    // サーバーのシャットダウンを制御するためのコンテキスト
    ctx, cancel := context.WithCancel(context.Background())
    server := &http.Server{
        Handler:      mux,
        ReadTimeout:  5 * time.Second,
        WriteTimeout: 10 * time.Second,
        IdleTimeout:  10 * time.Minute, // 10分間アイドルなら自動終了
        BaseContext:  func(_ net.Listener) context.Context { return ctx }, // リクエストコンテキストにシャットダウンコンテキストを渡す
    }
    server.RegisterOnShutdown(func() {
        log.Println("Web UIサーバーがシャットダウンしました。")
        serverMutex.Lock()
        currentServer = nil // サーバーが終了したらnilにする
        serverMutex.Unlock()
        cancel() // 関連するコンテキストもキャンセル
    })

    currentServer = &serverContext{listener: listener, server: server, port: port, shutdownCtx: ctx, shutdownCancel: cancel}

    // 4. サーバーをGoroutineで起動
    go func() {
        log.Printf("Web UIサーバーを http://127.0.0.1:%d で起動します。", port)
        // Serve()はブロッキングコールなので、エラーハンドリングが必要
        if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
            log.Printf("ERROR: Web UIサーバーが異常終了しました: %v", err)
            serverMutex.Lock()
            currentServer = nil // 異常終了時もnilにする
            serverMutex.Unlock()
        }
    }()

    // 5. ブラウザを開く
    if err := openBrowser(fmt.Sprintf("http://127.0.0.1:%d", port)); err != nil {
        log.Printf("WARNING: ブラウザの起動に失敗しました: %v。手動でURLを開いてください: http://127.0.0.1:%d", err, port)
        // ブラウザ起動失敗時でもサーバーは起動したままにする
    }
}

// handleConfig は /api/config へのリクエストを処理する
func handleConfig(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    switch r.Method {
    case http.MethodGet:
        // 設定ファイルを読み込んでJSONで返す
        cfg, err := config.LoadAndResolve("config.json") // 既存の関数を再利用
        if err != nil {
            log.Printf("ERROR: 設定ファイルの読み込みに失敗しました: %v", err)
            http.Error(w, `{"error": "設定ファイルの読み込みに失敗しました。ファイルが破損しているか、アクセスできません。"}`, http.StatusInternalServerError)
            return
        }
        if err := json.NewEncoder(w).Encode(cfg); err != nil {
            log.Printf("ERROR: 設定JSONのエンコードに失敗しました: %v", err)
            http.Error(w, `{"error": "設定データの準備中にエラーが発生しました。"}`, http.StatusInternalServerError)
            return
        }

    case http.MethodPost:
        // POSTされたJSONを解析して設定ファイルに保存
        var newCfg config.Config
        if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
            log.Printf("ERROR: 受信したJSONのデコードに失敗しました: %v", err)
            http.Error(w, `{"error": "無効なJSON形式です。入力データを確認してください。"}`, http.StatusBadRequest)
            return
        }

        // サーバーサイドでの詳細なバリデーション
        if validationErrors := validateConfig(&newCfg); len(validationErrors) > 0 {
            log.Printf("WARNING: 設定バリデーションエラー: %v", validationErrors)
            errorMsg := fmt.Sprintf("設定エラー: %s", strings.Join(validationErrors, ", "))
            http.Error(w, fmt.Sprintf(`{"error": "%s"}`, errorMsg), http.StatusBadRequest)
            return
        }

        // 設定ファイルを上書き保存する前にバックアップを作成 (エッジケース対策)
        backupPath := "config.json.bak"
        if err := copyFile("config.json", backupPath); err != nil {
            log.Printf("WARNING: config.jsonのバックアップ作成に失敗しました: %v", err)
            // バックアップ失敗は致命的ではないがログに残す
        }

        // 新しい設定をファイルに書き込む
        fileData, err := json.MarshalIndent(newCfg, "", "  ")
        if err != nil {
            log.Printf("ERROR: 新しい設定のJSONシリアライズに失敗しました: %v", err)
            http.Error(w, `{"error": "設定データの保存準備中にエラーが発生しました。"}`, http.StatusInternalServerError)
            return
        }
        if err := os.WriteFile("config.json", fileData, 0644); err != nil {
            log.Printf("ERROR: 設定ファイルの書き込みに失敗しました: %v", err)
            http.Error(w, `{"error": "設定ファイルの書き込みに失敗しました。ファイル権限を確認してください。"}`, http.StatusInternalServerError)
            return
        }

        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"message": "設定を正常に保存しました"}`))

    default:
        http.Error(w, `{"error": "許可されていないメソッドです"}`, http.StatusMethodNotAllowed)
    }
}

// handleShutdown はサーバーを安全にシャットダウンする
func handleShutdown(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, `{"error": "許可されていないメソッドです"}`, http.StatusMethodNotAllowed)
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"message": "サーバーをシャットダウンします"}`))

    // シャットダウンは非同期で行い、クライアントへのレスポンスをブロックしない
    go func() {
        time.Sleep(500 * time.Millisecond) // レスポンスを返すための猶予
        serverMutex.Lock()
        defer serverMutex.Unlock()
        if currentServer != nil && currentServer.server != nil {
            log.Println("Web UIサーバーのシャットダウンを開始します...")
            // currentServer.shutdownCtx を利用してシャットダウン
            if err := currentServer.server.Shutdown(currentServer.shutdownCtx); err != nil {
                log.Printf("ERROR: Web UIサーバーのシャットダウンに失敗しました: %v", err)
            }
        }
    }()
}

// validateConfig はConfig構造体の内容をバリデーションする
func validateConfig(cfg *config.Config) []string {
    var errors []string

    if cfg.GlobalMaxConcurrentTasks <= 0 {
        errors = append(errors, "GlobalMaxConcurrentTasksは1以上である必要があります。")
    }
    if cfg.MaxRequestsPerSecond <= 0 {
        errors = append(errors, "MaxRequestsPerSecondは0より大きい必要があります。")
    }
    if cfg.SafetyStopMinDiskGB < 0 {
        errors = append(errors, "SafetyStopMinDiskGBは0以上である必要があります。")
    }
    // TODO: 他のグローバル設定やタスクごとの設定に対する詳細なバリデーション
    // 例: パスが絶対パスか、ディレクトリとして有効か、URL形式が正しいかなど

    for i, task := range cfg.Tasks {
        if task.TaskName == "" {
            errors = append(errors, fmt.Sprintf("タスク%d: タスク名は必須です。", i+1))
        }
        if task.SiteAdapter == "" {
            errors = append(errors, fmt.Sprintf("タスク%d (%s): サイトアダプターは必須です。", i+1, task.TaskName))
        }
        if task.TargetBoardURL == "" {
            errors = append(errors, fmt.Sprintf("タスク%d (%s): ターゲットボードURLは必須です。", i+1, task.TaskName))
        } else if _, err := url.ParseRequestURI(task.TargetBoardURL); err != nil {
            errors = append(errors, fmt.Sprintf("タスク%d (%s): ターゲットボードURLの形式が不正です: %v", i+1, task.TaskName, err))
        }
        // パスの安全性チェック (ディレクトリトラバーサル防止)
        if task.SaveRootDirectory != "" && strings.Contains(task.SaveRootDirectory, "..") {
            errors = append(errors, fmt.Sprintf("タスク%d (%s): SaveRootDirectoryに無効な文字が含まれています。", i+1, task.TaskName))
        }
        // 他のパスも同様にチェック
    }

    return errors
}

// copyFile はファイルをコピーするヘルパー関数 (config.jsonバックアップ用)
func copyFile(src, dst string) error {
    sourceFile, err := os.Open(src)
    if err != nil {
        if os.IsNotExist(err) {
            return nil // 元ファイルが存在しない場合はエラーとしない
        }
        return fmt.Errorf("ソースファイルを開けません: %w", err)
    }
    defer sourceFile.Close()

    destFile, err := os.Create(dst)
    if err != nil {
        return fmt.Errorf("コピー先ファイルを作成できません: %w", err)
    }
    defer destFile.Close()

    _, err = io.Copy(destFile, sourceFile)
    if err != nil {
        return fmt.Errorf("ファイルのコピーに失敗しました: %w", err)
    }
    return destFile.Sync()
}

// openBrowser はOSのデフォルトブラウザでURLを開く (systray.goから移植/共通化)
func openBrowser(url string) error {
    var cmd *exec.Cmd
    switch runtime.GOOS {
    case "windows":
        cmd = exec.Command("cmd", "/c", "start", url)
    case "darwin":
        cmd = exec.Command("open", url)
    default: // Linux, BSD
        cmd = exec.Command("xdg-open", url)
    }
    // Start()はコマンドを非同期で実行し、すぐに制御を返す
    // Wait()を呼び出さない限り、子プロセスが終了するまで待たない
    if err := cmd.Start(); err != nil {
        return fmt.Errorf("ブラウザの起動コマンドの実行に失敗しました: %w", err)
    }
    return nil
}
```

#### 4.2. フロントエンド (`app.js`) (疑似コード)

```javascript
// app.js

document.addEventListener('DOMContentLoaded', () => {
    // グローバルな状態管理
    let state = {
        config: null,
    };

    // DOM要素
    const form = document.getElementById('config-form');
    const tasksContainer = document.getElementById('tasks-container');
    const addTaskBtn = document.getElementById('add-task-btn');
    const saveBtn = document.getElementById('save-btn');
    const statusMessage = document.getElementById('status-message'); // ステータス表示用要素

    // 初期化関数
    async function initialize() {
        try {
            const response = await fetch('/api/config');
            if (!response.ok) {
                const errorData = await response.json();
                throw new Error(errorData.error || '設定の取得に失敗しました');
            }
            state.config = await response.json();
            renderForm();
        } catch (error) {
            showStatus(`初期設定の読み込み中にエラーが発生しました: ${error.message}`, 'error');
        }
    }

    // フォームを描画する関数
    function renderForm() {
        // グローバル設定を描画
        document.querySelector('[name="config_version"]').value = state.config.config_version || '';
        document.querySelector('[name="global_max_concurrent_tasks"]').value = state.config.global_max_concurrent_tasks;
        // ... 他のグローバル設定も同様に ...

        // タスクを描画
        tasksContainer.innerHTML = ''; // コンテナをクリア
        state.config.tasks.forEach((task, index) => {
            const taskElement = createTaskElement(task, index);
            tasksContainer.appendChild(taskElement);
        });
    }

    // 個別のタスク要素を生成する関数
    function createTaskElement(task, index) {
        const div = document.createElement('div');
        div.className = 'task-box';
        div.dataset.index = index;
        div.innerHTML = `
            <h3>タスク: ${task.task_name || `新規タスク ${index + 1}`}</h3>
            <label>タスク名:</label><input type="text" name="task_name" value="${escapeHtml(task.task_name || '')}" placeholder="タスク名" required>
            <label>対象URL:</label><input type="url" name="target_board_url" value="${escapeHtml(task.target_board_url || '')}" placeholder="対象ボードURL" required>
            <!-- ... 他のタスク設定項目 ... -->
            <button type="button" class="remove-task-btn">このタスクを削除</button>
        `;
        return div;
    }

    // 保存処理
    async function handleSave() {
        showStatus('設定を保存中...', 'info');

        try {
            // フォームから最新のデータを読み取り、state.configを更新する
            const newConfig = serializeFormToConfig();

            const response = await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig),
            });

            const result = await response.json();

            if (!response.ok) {
                throw new Error(result.error || '設定の保存に失敗しました');
            }

            showStatus(result.message, 'success');
            
            // サーバーにシャットダウンを通知
            setTimeout(() => {
                fetch('/api/shutdown', { method: 'POST' })
                    .then(res => {
                        if (!res.ok) console.warn('サーバーシャットダウン通知に失敗しました');
                    })
                    .catch(err => console.warn('サーバーシャットダウン通知中にエラー:', err));
            }, 1000); // ユーザーがメッセージを読む猶予
            
            // 成功後、UIを無効化して再編集を防ぐ、またはリロードを促す
            saveBtn.disabled = true;
            addTaskBtn.disabled = true;

        } catch (error) {
            showStatus(`保存エラー: ${error.message}`, 'error');
            saveBtn.disabled = false; // エラー時は再試行可能にする
            addTaskBtn.disabled = false;
        }
    }

    // フォームの内容をJSONオブジェクトにシリアライズする
    function serializeFormToConfig() {
        const newConfig = { ...state.config }; // ベースコピー

        // グローバル設定の取得
        newConfig.config_version = document.querySelector('[name="config_version"]').value;
        newConfig.global_max_concurrent_tasks = parseInt(document.querySelector('[name="global_max_concurrent_tasks"]').value, 10);
        // ... 他のグローバル設定 ...

        // タスク設定の取得
        newConfig.tasks = [];
        document.querySelectorAll('.task-box').forEach(taskBox => {
            const task = {
                task_name: taskBox.querySelector('[name="task_name"]').value,
                target_board_url: taskBox.querySelector('[name="target_board_url"]').value,
                // ... 他のタスク設定 ...
            };
            newConfig.tasks.push(task);
        });

        return newConfig;
    }

    // ステータスメッセージを表示するヘルパー関数
    function showStatus(message, type) {
        statusMessage.textContent = message;
        statusMessage.className = `status-message ${type}`; // 'success', 'error', 'info'
        statusMessage.style.display = 'block';
        if (type !== 'info') { // 情報メッセージ以外は一定時間後に消す
            setTimeout(() => {
                statusMessage.style.display = 'none';
            }, 5000);
        }
    }

    // HTMLエスケープ関数 (XSS対策)
    function escapeHtml(text) {
        const div = document.createElement('div');
        div.appendChild(document.createTextNode(text));
        return div.innerHTML;
    }

    // イベントリスナー
    saveBtn.addEventListener('click', handleSave);
    
    addTaskBtn.addEventListener('click', () => {
        // 新しい空のタスクをstateに追加して再描画
        state.config.tasks.push({
            task_name: `新規タスク ${state.config.tasks.length + 1}`,
            site_adapter: "futaba", // デフォルト値
            // ... 他のデフォルト値 ...
        });
        renderForm(); // 再描画
    });

    tasksContainer.addEventListener('click', (e) => {
        if (e.target.classList.contains('remove-task-btn')) {
            const taskBox = e.target.closest('.task-box');
            const index = parseInt(taskBox.dataset.index, 10);
            if (!isNaN(index) && index >= 0 && index < state.config.tasks.length) {
                state.config.tasks.splice(index, 1); // stateから削除
                renderForm(); // 再描画
            } else {
                console.error("無効なタスク削除インデックス:", index);
            }
        }
    });

    // 初期化実行
    initialize();
});
```

### **4.1.3. バックエンドのエラーハンドリングとバリデーション**

バックエンドは、フロントエンドからのリクエストを処理する上で、堅牢なエラーハンドリングと厳格なバリデーションを適用する。

*   **`StartWebServer` のエラー処理:**
    *   `net.Listen`: ポートの確保に失敗した場合（例: OSのリソース不足、セキュリティポリシーによるブロック）、致命的なエラーとしてログに記録し、サーバー起動を中止する。
    *   `fs.Sub`: 埋め込みアセットの読み込みに失敗した場合、致命的なエラーとしてログに記録し、サーバー起動を中止する。
    *   `server.Serve`: サーバーの実行中に予期せぬエラーが発生した場合（`http.ErrServerClosed` 以外）、ログに記録し、`currentServer` を `nil` にリセットして再起動を可能にする。
    *   `openBrowser`: ブラウザの起動コマンドが失敗した場合、ログに警告を記録し、ユーザーに手動でURLを開くよう促すメッセージを表示する。サーバー自体は起動を継続する。

*   **`handleConfig` のエラー処理:**
    *   **`config.LoadAndResolve` (GETリクエスト時):**
        *   `config.json` が存在しない、読み取り権限がない、またはJSON形式が不正でデコードできない場合、`http.StatusInternalServerError` を返し、詳細なエラーメッセージをログに記録する。ユーザーには一般的なエラーメッセージを返す。
    *   **`json.NewDecoder(r.Body).Decode` (POSTリクエスト時):**
        *   受信したリクエストボディが有効なJSON形式でない場合、`http.StatusBadRequest` を返し、ログに記録する。
    *   **`validateConfig` (POSTリクエスト時):**
        *   ビジネスロジックに基づくバリデーションエラー（例: 必須フィールドの欠落、数値の範囲外、不正なURL形式など）が発生した場合、`http.StatusBadRequest` を返し、具体的なエラーメッセージのリストをフロントエンドに返す。
    *   **`copyFile` (POSTリクエスト時):**
        *   `config.json` のバックアップ作成に失敗した場合、警告としてログに記録するが、処理は続行する（バックアップ失敗は致命的ではないため）。
    *   **`json.MarshalIndent` (POSTリクエスト時):**
        *   `config.Config` 構造体をJSON文字列に変換する際にエラーが発生した場合、`http.StatusInternalServerError` を返し、ログに記録する。
    *   **`os.WriteFile` (POSTリクエスト時):**
        *   `config.json` への書き込みに失敗した場合（例: ディスクフル、書き込み権限不足）、`http.StatusInternalServerError` を返し、ログに記録する。

*   **`handleShutdown` のエラー処理:**
    *   `server.Shutdown`: サーバーのシャットダウン中にエラーが発生した場合、ログに記録する。これは通常、既にサーバーが停止している場合などに発生する可能性がある。

*   **エラーレスポンス形式:**
    *   すべてのAPIエラーレスポンスは、`{"error": "エラーメッセージ"}` のJSON形式で統一する。

##### **`validateConfig` 関数 (詳細):**

この関数は、`config.Config` 構造体の各フィールドに対して、以下の種類のバリデーションを行う。

*   **型チェック:** 数値型が正しくパースされているか、ブール値が有効か。
*   **範囲チェック:** `GlobalMaxConcurrentTasks` や `MaxRequestsPerSecond` などが適切な範囲内にあるか（例: 0より大きい）。
*   **必須フィールドチェック:** `TaskName`, `SiteAdapter`, `TargetBoardURL` などが空でないか。
*   **形式チェック:** `TargetBoardURL` が有効なURL形式であるか。
*   **パスの安全性チェック:**
    *   `SaveRootDirectory`, `HistoryFilePath` など、ユーザーが指定するファイルパスに `..` (親ディレクトリ参照) や絶対パス指定（意図しない場合）が含まれていないかを確認し、ディレクトリトラバーサル攻撃を防ぐ。
    *   `filepath.Clean` を使用してパスを正規化し、`strings.HasPrefix` で意図したルートディレクトリ以下に限定されているかを確認する。
    *   OS固有のパス区切り文字 (`/`, `\`) を適切に処理する。

#### 4.2.1. フロントエンドのエラー表示

フロントエンドは、バックエンドからのエラーレスポンスを適切に解釈し、ユーザーに分かりやすい形でフィードバックを提供する。

*   **`showStatus` 関数:**
    *   成功、情報、エラーの3種類のメッセージタイプをサポートし、それぞれ異なるスタイル（色など）で表示する。
    *   エラーメッセージは、ユーザーが内容を確認できるよう、一定時間表示後に自動で消えるか、ユーザーが閉じるまで表示され続けるようにする。
    *   APIからのエラーレスポンスに含まれる詳細なエラーメッセージをそのまま表示する。
*   **APIリクエスト時のエラーハンドリング:**
    *   `fetch` APIがネットワークエラー（例: サーバーが起動していない、接続が切れた）を返した場合、`catch` ブロックで捕捉し、`showStatus` を使ってユーザーに通知する。
    *   `response.ok` が `false` の場合、レスポンスボディからエラーJSONをパースし、その中のエラーメッセージを抽出して表示する。
*   **フォームの入力バリデーション:**
    *   HTML5の標準バリデーション属性 (`required`, `type="url"`, `min`, `max` など) を活用し、クライアントサイドで基本的な入力チェックを行う。
    *   JavaScriptでより複雑なリアルタイムバリデーションを実装し、ユーザーが不正な値を入力した際に即座にフィードバックを提供する。

### **5. 段階的実装計画 (Phased Implementation Plan)**

1.  **フェーズ1: バックエンド基盤の構築**
    *   `internal/webui` ディレクトリと `webui.go` を作成。
    *   `embed` を用いたアセット埋め込みを実装。
    *   空きポートで起動し、`127.0.0.1` にバインドする最小限のWebサーバーを実装。
    *   `index.html` を提供するルートハンドラを実装。
    *   `StartWebServer` 内で `net.Listen` や `fs.Sub` のエラーを適切にログに記録し、サーバー起動を中止する。
    *   Systrayに「設定」メニューを追加し、`StartWebServer` を呼び出すようにする。`openBrowser` のエラーもハンドリングする。

2.  **フェーズ2: データ表示 (Read-Only)**
    *   `GET /api/config` エンドポイントを実装。`config.LoadAndResolve` のエラーを適切にハンドリングし、`http.StatusInternalServerError` を返す。
    *   `app.js` で上記APIを叩き、取得したデータを元に、静的なHTMLフォームを生成するロジックを実装する。
    *   フロントエンドで `fetch` のエラーやAPIからのエラーレスポンスを `showStatus` で表示する。

3.  **フェーズ3: データ編集と保存 (Write)**
    *   `POST /api/config` エンドポイントを実装し、受信JSONのデコードエラー、`validateConfig` によるバリデーションエラー、`json.MarshalIndent` エラー、`os.WriteFile` エラーを適切にハンドリングし、適切なHTTPステータスコードとエラーメッセージを返す。
    *   `config.json` 保存前のバックアップ処理を実装し、失敗時は警告ログを出す。
    *   フロントエンドで「保存」ボタンのロジックを実装し、フォームの内容をJSONとしてPOSTできるようにする。APIからの成功/エラーレスポンスを `showStatus` で表示する。

4.  **フェーズ4: 動的UIとUX向上**
    *   タスクの「追加」「削除」ボタンと、それに対応するJavaScriptロジックを実装する。
    *   フロントエンドでのリアルタイムバリデーションとエラー表示を強化する。
    *   保存後のステータス表示、サーバーの自動シャットダウン機能を実装する。`handleShutdown` のエラーもログに記録する。

### **6. リスクと対策 (Risks and Mitigations)**

*   **リスク:** ポートが他のアプリケーションによって使用されている。
    *   **対策:** `net.Listen("tcp", "127.0.0.1:0")` を使用することで、OSに空きポートを自動で選択させる。これにより、ポート競合のリスクを最小化する。
*   **リスク:** `config.json` が破損または不正な状態になる。
    *   **対策:** `POST /api/config` 時に、既存の `config.json` を `config.json.bak` としてバックアップする。書き込み失敗時には、バックアップから復元する手動プロセスをユーザーに案内できるようにする。
*   **リスク:** ブラウザの起動に失敗する。
    *   **対策:** `openBrowser` 関数がエラーを返した場合、ログに警告を記録し、ユーザーにWeb UIのURLを直接表示して手動で開くよう促す。
*   **リスク:** Webサーバーがタイムアウトで終了した後、ブラウザタブが開きっぱなしになる。
    *   **対策:** サーバーがシャットダウンしたことをブラウザ側で検知することは困難なため、ユーザーがタブを閉じることを推奨する。また、保存成功時に `POST /api/shutdown` を呼び出すことで、ユーザーがタブを閉じなくてもサーバーが速やかに終了するように促す。
*   **リスク:** 複数のGIBAインスタンスが同時に起動し、それぞれがWeb UIを起動しようとする。
    *   **対策:** `serverMutex` と `currentServer` 変数を用いて、GIBAプロセス内でWeb UIサーバーが単一インスタンスであることを保証する。2つ目以降の起動要求は、既存のサーバーのURLを再度開くだけにする。
*   **リスク:** ユーザーが保存せずにブラウザタブを閉じた場合、変更が失われる。
    *   **対策:** Web標準の `beforeunload` イベントを利用して、未保存の変更がある場合にユーザーに警告を出すことを検討する。ただし、これはUXを損なう可能性もあるため、慎重に導入する。

### **7. エラーハンドリング戦略 (Error Handling Strategy)**

GIBAのWeb UIにおけるエラーハンドリングは、以下の原則に基づき実装される。

*   **Go側 (バックエンド):**
    *   **エラーラッピング:** `fmt.Errorf("%w", err)` を使用し、エラーチェーンを保持して根本原因を追跡可能にする。
    *   **ロギング:** すべてのエラーは `log` パッケージを通じて適切にログに記録される。特に、ユーザーに返すべきでない詳細なシステムエラーはログのみに記録する。
    *   **HTTPステータスコード:** クライアントへのレスポンスには、エラーの種類に応じた適切なHTTPステータスコード（例: `400 Bad Request`, `404 Not Found`, `500 Internal Server Error`）を使用する。
    *   **汎用エラーメッセージ:** クライアントには、セキュリティ上の理由から、詳細なシステムエラーメッセージではなく、一般的なエラーメッセージを返す。
    *   **リトライ可能性の考慮:** ネットワーク関連のエラーなど、一時的な問題でリトライ可能なエラーについては、ログにその旨を記録する。

*   **フロントエンド側 (JavaScript):**
    *   **ユーザーへのフィードバック:** `showStatus` 関数を通じて、成功、情報、エラーの各メッセージをユーザーに明確に表示する。
    *   **APIエラーの解釈:** バックエンドから返されるJSON形式のエラーレスポンスをパースし、ユーザーフレンドリーなメッセージに変換して表示する。
    *   **入力バリデーションフィードバック:** リアルタイムバリデーションにより、ユーザーが不正な値を入力した際に即座に視覚的なフィードバック（例: 赤い枠線、エラーテキスト）を提供する。
    *   **リトライメカニズム:** 保存失敗時など、ユーザーが操作を再試行できるようなUIを提供する（例: 保存ボタンの再有効化）。

### **8. エッジケース考慮事項 (Edge Case Considerations)**

*   **`config.json` の破損/不正な状態:**
    *   **GET時:** `config.LoadAndResolve` がエラーを返した場合、UIはエラーメッセージを表示し、編集フォームを無効化するか、デフォルト値で初期化してユーザーに新規作成を促す。
    *   **POST時:** バリデーションエラーや書き込みエラーが発生した場合、バックアップからの復元を検討するか、ユーザーに手動での修正を促す。
*   **ディスク容量不足、ファイルパーミッションエラー:**
    *   `os.WriteFile` がこれらのエラーを返した場合、バックエンドは `http.StatusInternalServerError` を返し、フロントエンドはユーザーに「設定ファイルの保存に失敗しました。ディスク容量またはファイル権限を確認してください。」といった具体的なメッセージを表示する。
*   **ブラウザ起動失敗:**
    *   `openBrowser` がエラーを返した場合、GIBAはログに警告を記録し、システムトレイのツールチップや一時的な通知で、Web UIのURLをユーザーに提示し、手動でブラウザに貼り付けるよう促す。
*   **Webサーバーのタイムアウトとブラウザの挙動:**
    *   サーバーがアイドルタイムアウトで終了した後、ユーザーがブラウザで操作を試みると、ブラウザは「接続できません」といったエラーを表示する。これは正常な挙動であり、ユーザーには再度システムトレイから設定を開くよう案内する。
*   **複数のGIBAインスタンス起動時の挙動:**
    *   GIBAは単一インスタンスでの実行を前提とするが、もし複数起動された場合、Web UIサーバーは最初のインスタンスのみが起動し、他のインスタンスからの要求は既存のUIを開くようにする（`serverMutex` と `currentServer` で制御）。
*   **ユーザーが保存せずにブラウザを閉じた場合:**
    *   未保存の変更がある場合、`beforeunload` イベントで警告を出すことは可能だが、ユーザーが「変更を破棄」を選択した場合はデータは失われる。これはユーザーの選択として受け入れる。
*   **タスクテンプレートの動的な追加/削除時のUI同期:**
    *   フロントエンドは、タスクの追加・削除時に `state.config.tasks` を更新し、`renderForm()` を再呼び出しすることでUIを同期する。これにより、DOM要素のインデックスと `state` の整合性を保つ。

### **9. セキュリティ考慮事項とリスク対策 (Security Considerations and Risk Mitigations)**

「ポートを開けることはリスクが高い」というご指摘は非常に重要であり、以下の対策を講じることでそのリスクを最小限に抑える。

*   **ポートの安全性:**
    *   **ループバックアドレスへの限定:** Webサーバーは `127.0.0.1` (IPv4) または `::1` (IPv6) のループバックアドレスにのみバインドする。これにより、外部ネットワークからのアクセスを物理的に遮断し、GIBAが動作しているローカルマシンからのみアクセス可能とする。これは最も基本的ながら最も効果的なセキュリティ対策である。
    *   **動的ポート割り当て:** `net.Listen("tcp", "127.0.0.1:0")` を使用して、OSに空いているポートを動的に割り当てさせる。これにより、既知のポートを固定で使用することによるポートスキャンや競合のリスクを回避する。
*   **入力バリデーション (サーバーサイド):**
    *   フロントエンドでのバリデーションはユーザー体験向上のためだが、セキュリティの最終防衛線は常にサーバーサイドである。
    *   `validateConfig` 関数で、すべての受信データに対して厳格な型チェック、範囲チェック、形式チェック、必須項目チェックを行う。
    *   特に、数値フィールドには `strconv.Atoi` や `strconv.ParseFloat` を使用し、非数値が渡された場合はエラーとする。
    *   ブール値は `true`/`false` 以外の値を受け付けない。
*   **パスのサニタイズとディレクトリトラバーサル防止:**
    *   `SaveRootDirectory`, `HistoryFilePath`, `LogFilePath` など、ユーザーがファイルパスを指定できるすべての設定項目について、サーバーサイドで厳格なサニタイズを行う。
    *   `filepath.Clean` を使用してパスを正規化し、`strings.Contains(path, "..")` や `filepath.IsAbs(path)` をチェックして、意図しない親ディレクトリへのアクセスや絶対パス指定を防止する。
    *   設定されたルートディレクトリ (`SaveRootDirectory` など) の外側への書き込みを許容しないロジックを実装する。
*   **情報漏洩防止:**
    *   `GET /api/config` エンドポイントは、`config.json` の内容をそのまま返すため、設定ファイルに機密情報（パスワードなど）を含めない設計とする。もし含める必要がある場合は、APIレスポンスからそれらのフィールドを除外するフィルタリングを実装する。
    *   サーバーのエラーメッセージは、デバッグ情報やシステム内部のパスなどをユーザーに開示しないよう、汎用的な表現に留める。
*   **CSRF (Cross-Site Request Forgery) 対策:**
    *   Webサーバーがループバックアドレスに限定されているため、外部ドメインからのリクエストはブラウザのSame-Origin Policyによりブロックされる。そのため、一般的なCSRFトークンの導入は必須ではないが、より厳格なセキュリティを求める場合は、`index.html` にCSRFトークンを埋め込み、`POST` リクエスト時に検証するメカニズムを導入することも可能である。
*   **XSS (Cross-Site Scripting) 対策:**
    *   フロントエンドで、ユーザー入力やバックエンドから取得したデータ（例: タスク名、URL）をHTMLに表示する際は、必ずHTMLエスケープ処理 (`escapeHtml` 関数など) を行う。これにより、悪意のあるスクリプトが挿入されるのを防ぐ。
*   **権限昇格:**
    *   GIBAはユーザー権限で実行されることを前提とする。Web UIサーバーも同じユーザー権限で動作するため、サーバー経由でシステムレベルの操作が行われるリスクは、GIBA本体が持つリスクと同等である。不必要な高権限での実行は避ける。
