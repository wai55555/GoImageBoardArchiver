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
	"os/exec"
	"runtime"
	"sync"
	"time"

	"GoImageBoardArchiver/internal/config"
)

//go:embed embed/*
var embeddedAssets embed.FS

// serverContext はWebサーバーのインスタンスと状態を管理します。
type serverContext struct {
	server   *http.Server
	listener net.Listener
	port     int
}

var (
	currentServer *serverContext
	serverMutex   sync.Mutex // サーバーインスタンスへの同時アクセスを保護します。
)

// StartWebServer はWebサーバーを非同期で起動し、ブラウザを開きます。
// すでにサーバーが起動している場合は、新しいブラウザタブで既存のサーバーのURLを開くだけです。
func StartWebServer() {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	if currentServer != nil {
		log.Println("Web UIサーバーはすでに起動しています。既存のサーバーを利用します。")
		if err := openBrowser(fmt.Sprintf("http://127.0.0.1:%d", currentServer.port)); err != nil {
			log.Printf("WARNING: ブラウザの起動に失敗しました: %v", err)
		}
		return
	}

	// 空きポートを検索します。 "127.0.0.1:0" を指定するとOSが自動で空きポートを選択します。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("FATAL: Web UIサーバーの起動に失敗しました: 空きポートでのリッスンに失敗しました: %v", err)
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()

	// APIエンドポイント
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/shutdown", handleShutdown)

	// 静的ファイル用のハンドラ (CSS, JS)
	staticFS, err := fs.Sub(embeddedAssets, "embed/static")
	if err != nil {
		log.Printf("FATAL: 埋め込み静的ファイルの読み込みに失敗しました: %v", err)
		listener.Close()
		return
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// ルートハンドラ (index.html)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		indexHTML, err := embeddedAssets.ReadFile("embed/index.html")
		if err != nil {
			log.Printf("ERROR: index.htmlの読み込みに失敗しました: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  10 * time.Minute, // 10分間アイドルなら自動終了
	}

	server.RegisterOnShutdown(func() {
		log.Println("Web UIサーバーがシャットダウンしました。")
		serverMutex.Lock()
		currentServer = nil // サーバーが終了したらnilにリセットします。
		serverMutex.Unlock()
	})

	currentServer = &serverContext{
		server:   server,
		listener: listener,
		port:     port,
	}

	// サーバーをGoroutineで起動します。
	go func() {
		log.Printf("Web UIサーバーを http://127.0.0.1:%d で起動します。", port)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("ERROR: Web UIサーバーが異常終了しました: %v", err)
			serverMutex.Lock()
			currentServer = nil // 異常終了時もリセットします。
			serverMutex.Unlock()
		}
	}()

	// ブラウザでURLを開きます。
	if err := openBrowser(fmt.Sprintf("http://127.0.0.1:%d", port)); err != nil {
		log.Printf("WARNING: ブラウザの起動に失敗しました: %v。手動でURLを開いてください: http://127.0.0.1:%d", err, port)
	}
}

// handleConfig は /api/config へのリクエストを処理します。
func handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		// 設定ファイルを読み込んでJSONで返します。
		cfg, err := config.LoadAndResolve("config.json")
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
		// POSTされたJSONを解析して設定ファイルに保存します。
		var newCfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			log.Printf("ERROR: 受信したJSONのデコードに失敗しました: %v", err)
			http.Error(w, `{"error": "無効なJSON形式です。入力データを確認してください。"}`, http.StatusBadRequest)
			return
		}

		// TODO: ここで詳細なバリデーションロジックを実装

		// 新しい設定をファイルに書き込みます。
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

// handleShutdown はサーバーを安全にシャットダウンします。
func handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "許可されていないメソッドです"}`, http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "サーバーをシャットダウンします"}`))

	// シャットダウンは非同期で行い、クライアントへのレスポンスをブロックしません。
	go func() {
		time.Sleep(500 * time.Millisecond) // レスポンスを返すための猶予
		serverMutex.Lock()
		defer serverMutex.Unlock()
		if currentServer != nil && currentServer.server != nil {
			log.Println("Web UIサーバーのシャットダウンを開始します...")
			if err := currentServer.server.Shutdown(context.Background()); err != nil {
				log.Printf("ERROR: Web UIサーバーのシャットダウンに失敗しました: %v", err)
			}
		}
	}()
}

// openBrowser はOSのデフォルトブラウザでURLを開きます。
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // Linux, BSDなど
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ブラウザの起動コマンドの実行に失敗しました: %w", err)
	}
	return nil
}
