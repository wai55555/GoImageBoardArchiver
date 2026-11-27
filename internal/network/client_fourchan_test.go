package network

// 4chan 固有の通信仕様 (Phase 6 / REQ-6.5) のテスト。
// 実 API には接続せず、RoundTripper を差し替えて検証する。

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"GoImageBoardArchiver/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestClient(t *testing.T, settings config.NetworkSettings) *Client {
	t.Helper()
	c, err := NewClient(settings)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func jsonResponse(req *http.Request, status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// TestFourChanIfModifiedSince は、初回は IMS を送らず、2回目は前回の
// Last-Modified を送り、304 を ErrNotModified として扱うことを検証します。
func TestFourChanIfModifiedSince(t *testing.T) {
	c := newTestClient(t, config.NetworkSettings{})

	var mu sync.Mutex
	var seenIMS []string
	call := 0
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		call++
		seenIMS = append(seenIMS, req.Header.Get("If-Modified-Since"))
		if call == 1 {
			return jsonResponse(req, http.StatusOK, `[]`, http.Header{
				"Last-Modified": {"Wed, 21 Oct 2015 07:28:00 GMT"},
			}), nil
		}
		return jsonResponse(req, http.StatusNotModified, "", nil), nil
	})

	url := "https://a.4cdn.org/vg/catalog.json"
	body, err := c.Get(context.Background(), url)
	if err != nil || body != "[]" {
		t.Fatalf("1回目: body=%q err=%v", body, err)
	}

	_, err = c.Get(context.Background(), url)
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("2回目は ErrNotModified を返すべきです: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenIMS) != 2 {
		t.Fatalf("リクエスト数: got %d, want 2", len(seenIMS))
	}
	if seenIMS[0] != "" {
		t.Errorf("初回に If-Modified-Since を送ってはいけません: %q", seenIMS[0])
	}
	if seenIMS[1] != "Wed, 21 Oct 2015 07:28:00 GMT" {
		t.Errorf("2回目に前回の Last-Modified を送るべきです: %q", seenIMS[1])
	}
}

// TestFourChanNoIMSOnCDN は、i.4cdn.org (メディア) には IMS を送らないことを検証します。
func TestFourChanNoIMSOnCDN(t *testing.T) {
	c := newTestClient(t, config.NetworkSettings{})
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("If-Modified-Since"); got != "" {
			t.Errorf("i.4cdn.org に IMS を送っています: %q", got)
		}
		return jsonResponse(req, http.StatusOK, "binary", nil), nil
	})
	if _, err := c.Get(context.Background(), "https://i.4cdn.org/vg/1700000000123.png"); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

// TestNonFourChan304IsNotNormalized は、ふたば等のホストでは 304 を
// 従来どおり HTTPError として扱う (挙動を変えない) ことを検証します。
func TestNonFourChan304IsNotNormalized(t *testing.T) {
	c := newTestClient(t, config.NetworkSettings{})
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, http.StatusNotModified, "", nil), nil
	})
	_, err := c.Get(context.Background(), "https://may.2chan.net/b/futaba.php?mode=cat")
	if err == nil || errors.Is(err, ErrNotModified) {
		t.Fatalf("ふたばの 304 は ErrNotModified ではなく HTTPError であるべきです: %v", err)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotModified {
		t.Fatalf("HTTPError(304) を期待しました: %v", err)
	}
}

// TestFourChanCombinedLimiter は、a.4cdn.org と i.4cdn.org が
// 1つのリミッターを共有し、間隔が 1 秒以上になることを検証します。
func TestFourChanCombinedLimiter(t *testing.T) {
	c := newTestClient(t, config.NetworkSettings{
		PerDomainIntervalMillis: map[string]int{
			"a.4cdn.org":    250,  // 1秒未満は無視される
			"i.4cdn.org":    1500, // こちらが優先される
			"may.2chan.net": 700,
		},
	})

	api := c.getLimiterForHost("a.4cdn.org")
	cdn := c.getLimiterForHost("i.4cdn.org")
	if api != cdn {
		t.Fatal("a.4cdn.org と i.4cdn.org は同じリミッターを共有すべきです")
	}
	if got := api.Limit(); got > 1 {
		t.Errorf("合算レートが 1req/s を超えています: %v", got)
	}
	// 1500ms 間隔 = 0.666... req/s 程度
	if got, want := float64(api.Limit()), 1.0/1.5; got > want+0.01 || got < want-0.01 {
		t.Errorf("合算間隔が設定の最大値になっていません: got %v, want ~%v", got, want)
	}

	other := c.getLimiterForHost("may.2chan.net")
	if other == api {
		t.Fatal("4chan 以外のホストは別のリミッターを持つべきです")
	}
}

// TestFourChanThreadRefetchWait は、同一スレッドの再取得間隔 (10 秒) の判定を検証します。
func TestFourChanThreadRefetchWait(t *testing.T) {
	c := newTestClient(t, config.NetworkSettings{})
	now := time.Now()
	threadURL := "https://a.4cdn.org/vg/thread/1001.json"

	if got := c.fourChanThreadWait(threadURL, "a.4cdn.org", now); got != 0 {
		t.Fatalf("未取得の URL は待たない: %v", got)
	}

	c.recordFetch(threadURL, "Wed, 21 Oct 2015 07:28:00 GMT", now.Add(-4*time.Second))
	if got := c.fourChanThreadWait(threadURL, "a.4cdn.org", now); got != 6*time.Second {
		t.Errorf("残り 6 秒待つべきです: got %v", got)
	}

	c.recordFetch(threadURL, "", now.Add(-11*time.Second))
	if got := c.fourChanThreadWait(threadURL, "a.4cdn.org", now); got != 0 {
		t.Errorf("10 秒以上経過していれば待たない: %v", got)
	}

	// カタログや他ホストは対象外。
	c.recordFetch("https://a.4cdn.org/vg/catalog.json", "", now.Add(-time.Second))
	if got := c.fourChanThreadWait("https://a.4cdn.org/vg/catalog.json", "a.4cdn.org", now); got != 0 {
		t.Errorf("カタログはスレッド間隔の対象外: %v", got)
	}
}

// TestCatboxUsesNeutralUserAgent は、catbox 宛だけ設定 UA ではなく中立 UA を
// 送ることを検証します (Chrome 系 UA だと catbox が EOF を返すため)。
func TestCatboxUsesNeutralUserAgent(t *testing.T) {
	c := newTestClient(t, config.NetworkSettings{UserAgent: "Mozilla/5.0 Chrome/120.0.0.0"})
	var got []string
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = append(got, req.Header.Get("User-Agent"))
		return jsonResponse(req, http.StatusOK, "x", nil), nil
	})

	if _, err := c.Get(context.Background(), "https://files.catbox.moe/abc123.png"); err != nil {
		t.Fatalf("Get(catbox): %v", err)
	}
	if _, err := c.Get(context.Background(), "https://i.4cdn.org/vg/1700000000123.png"); err != nil {
		t.Fatalf("Get(4cdn): %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("リクエスト数: got %d, want 2", len(got))
	}
	if got[0] != neutralUserAgent {
		t.Errorf("catbox には中立 UA を送るべきです: %q", got[0])
	}
	if got[1] != "Mozilla/5.0 Chrome/120.0.0.0" {
		t.Errorf("4cdn には設定 UA を送るべきです: %q", got[1])
	}
}
