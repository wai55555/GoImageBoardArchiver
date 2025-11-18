package network

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wai55555/GoImageBoardArchiver/internal/config"
)

func TestClient_CookieIntegration(t *testing.T) {
	// 1. Arrange (準備) - ダミーサーバーの構築
	expectedCookieValue := "9x100x20x0x0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// リクエストから'cxyl' Cookieを取得
		cookie, err := r.Cookie("cxyl")
		if err != nil {
			// Cookieがなければエラーを返す
			http.Error(w, "Cookie 'cxyl' not found", http.StatusBadRequest)
			t.Errorf("サーバー: リクエストに'cxyl' Cookieが見つかりませんでした。")
			return
		}

		// Cookieの値が期待通りか検証
		if cookie.Value != expectedCookieValue {
			http.Error(w, "Invalid cookie value", http.StatusBadRequest)
			t.Errorf("サーバー: Cookieの値が期待値と異なります。期待値: %s, 実際値: %s", expectedCookieValue, cookie.Value)
			return
		}

		// 成功したら"Success"を返す
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Success"))
	}))
	defer server.Close()

	// 2. Arrange (準備) - テスト対象クライアントの作成
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClientの作成に失敗しました: %v", err)
	}

	// テスト用の設定値
	settings := config.FutabaCatalogSettings{
		Cols:        9,
		Rows:        100,
		TitleLength: 20,
	}

	// 新しいSetCookieメソッドに渡すhttp.Cookieオブジェクトを生成
	cookieToSet := &http.Cookie{
		Name:  "cxyl",
		Value: fmt.Sprintf("%dx%dx%dx0x0", settings.Cols, settings.Rows, settings.TitleLength),
		Path:  "/",
		// DomainはSetCookieメソッド内で適切に処理されるため、ここでは設定しない
	}

	// ダミーサーバーのURLに対してCookieを設定
	if err := client.SetCookie(server.URL, cookieToSet); err != nil { // SetFutabaCatalogCookie -> SetCookie
		t.Fatalf("SetCookieで予期せぬエラーが発生しました: %v", err)
	}

	// 3. Act (実行)
	// ダミーサーバーにGETリクエストを送信
	body, err := client.Get(server.URL)

	// 4. Assert (検証)
	if err != nil {
		t.Fatalf("client.Getで予期せぬエラーが発生しました: %v", err)
	}

	if body != "Success" {
		t.Errorf("レスポンスボディが期待値と異なります。期待値: 'Success', 実際値: '%s'", body)
	}
}
