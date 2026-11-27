// Package network は、GIBAのHTTP通信に関する機能を提供します。
// Cookie Jarによるセッション管理をカプセル化した、より高レベルな
// HTTPクライアントを実装しています。
package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"GoImageBoardArchiver/internal/config"

	"golang.org/x/time/rate"
)

// ErrNotModified は、条件付き GET (If-Modified-Since) に対して 304 が返ったことを表します。
// 呼び出し側は「更新なし・正常」として扱い、エラー / リトライ対象にしないでください。
var ErrNotModified = errors.New("not modified (304)")

// 4chan API のホストと制限値。
const (
	fourChanAPIHost         = "a.4cdn.org"
	fourChanCDNHost         = "i.4cdn.org"
	fourChanCombinedLimiter = "4cdn.org (combined)"
	// 公式ルール: a.4cdn.org / i.4cdn.org の合算で 1 リクエスト/秒以下。
	fourChanMinCombinedInterval = time.Second
	// 公式ルール: 同一スレッドの再取得は 10 秒以上あける。
	fourChanMinThreadInterval = 10 * time.Second
)

// fourChanExternalHosts は 4chan の AI 画像スレで使われる外部メディアホストです。
// catbox は Chrome 系の User-Agent を bot とみなして接続を切る (EOF) ため、
// これらのホスト宛だけは中立の User-Agent を使う。
var fourChanExternalHosts = map[string]bool{
	"files.catbox.moe":     true,
	"litter.catbox.moe":    true,
	"litterbox.catbox.moe": true,
}

// neutralUserAgent は catbox など UA で弾くホスト向けの識別子です。
const neutralUserAgent = "GoImageBoardArchiver/1.0 (+https://github.com/wai55555/GoImageBoardArchiver)"

// fourChanThreadPathPattern は a.4cdn.org のスレッド JSON のパス
// (/<board>/thread/<no>.json) にマッチします。
var fourChanThreadPathPattern = regexp.MustCompile(`^/[^/]+/thread/\d+\.json$`)

// HTTPError は、HTTPリクエストで発生したエラーとステータスコードを保持します。
type HTTPError struct {
	StatusCode int
	URL        string
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s (URL: %s)", e.StatusCode, e.Message, e.URL)
}

// IsRetryable は、このエラーがリトライ可能かどうかを判定します。
// 4xxエラー（クライアントエラー）はリトライ不可、5xxエラー（サーバーエラー）はリトライ可能とします。
func (e *HTTPError) IsRetryable() bool {
	// 400番台のエラーはクライアント側の問題なのでリトライしても無駄
	// 404 Not Found, 403 Forbidden, 410 Gone など
	if e.StatusCode >= 400 && e.StatusCode < 500 {
		return false
	}
	// 500番台のエラーはサーバー側の一時的な問題の可能性があるのでリトライ可能
	// 503 Service Unavailable, 502 Bad Gateway など
	return true
}

// Client は、Cookie Jarを内包し、HTTPセッションを管理するクライアントです。
type Client struct {
	httpClient         *http.Client
	jar                *cookiejar.Jar
	userAgent          string
	defaultHeaders     map[string]string
	rateLimiters       map[string]*rate.Limiter // ホスト名ごとのレートリミッター
	rateLimitersMutex  sync.Mutex               // rateLimitersへのアクセスを保護するMutex
	perDomainIntervals map[string]int           // ドメインごとの設定間隔

	// 条件付き GET 用の状態 (URL 単位)。4chan API (a.4cdn.org) のみ使用する。
	conditional      map[string]*conditionalEntry
	conditionalMutex sync.Mutex
}

// conditionalEntry は URL 単位の最終更新情報です。
type conditionalEntry struct {
	lastModified string    // 前回レスポンスの Last-Modified
	lastFetched  time.Time // 前回取得時刻 (スレッド再取得間隔の判定用)
}

// NewClient は NetworkSettings に基づいて HTTP クライアントを初期化し、
// ドメインごとのレートリミッターを設定します。
func NewClient(settings config.NetworkSettings) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jarの作成に失敗しました: %w", err)
	}

	// RequestTimeoutMillisをtime.Durationに変換
	timeout := time.Duration(settings.RequestTimeoutMillis) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second // デフォルトタイムアウト
	}

	httpClient := &http.Client{
		Jar:     jar,
		Timeout: timeout, // タイムアウトを設定
	}

	// ドメインごとのレートリミッターを構築
	rateLimiters := make(map[string]*rate.Limiter)
	for domain, intervalMillis := range settings.PerDomainIntervalMillis {
		if intervalMillis <= 0 {
			continue
		}
		// intervalMillis 毎に 1 リクエストを許可する limiter
		limiter := rate.NewLimiter(rate.Every(time.Duration(intervalMillis)*time.Millisecond), 1)
		rateLimiters[domain] = limiter
	}

	return &Client{
		httpClient:         httpClient,
		jar:                jar,
		userAgent:          settings.UserAgent,
		defaultHeaders:     settings.DefaultHeaders,
		rateLimiters:       rateLimiters,
		perDomainIntervals: settings.PerDomainIntervalMillis,
		conditional:        make(map[string]*conditionalEntry),
	}, nil
}

// SetCookie は、指定されたURLのドメインに対して、任意のCookieを設定します。
func (c *Client) SetCookie(domainURL string, cookie *http.Cookie) error {
	if !strings.HasPrefix(domainURL, "http") {
		domainURL = "https://" + domainURL
	}

	parsedURL, err := url.Parse(domainURL)
	if err != nil {
		return fmt.Errorf("Cookie設定のためのURL解析に失敗しました: %w", err)
	}

	c.jar.SetCookies(parsedURL, []*http.Cookie{cookie})
	return nil
}

// Get は、設定済みのCookieを使って指定されたURLにGETリクエストを送信し、
// レスポンスボディを文字列として返します。
func (c *Client) Get(ctx context.Context, reqURL string) (string, error) {
	parsedURL, err := url.Parse(reqURL)
	if err != nil {
		return "", fmt.Errorf("リクエストURLの解析に失敗しました (%s): %w", reqURL, err)
	}

	host := strings.ToLower(parsedURL.Hostname())

	// 4chan の同一スレッド再取得は 10 秒以上あける (公式ルール)。
	if wait := c.fourChanThreadWait(reqURL, host, time.Now()); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// ドメインごとのレートリミッターを取得し、待機
	limiter := c.getLimiterForHost(host)

	// 修正: ここでのロックは不要かつデッドロックの原因となるため削除
	// getLimiterForHost内でロックは適切に処理されており、limiter.Waitはスレッドセーフ

	if err := limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("レートリミッター待機中にエラーが発生しました: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("GETリクエストの作成に失敗しました (%s): %w", reqURL, err)
	}

	// デフォルトヘッダーを全て設定
	for key, value := range c.defaultHeaders {
		req.Header.Set(key, value)
	}
	// User-Agentも設定。catbox は Chrome 系 UA を弾くため中立 UA に切り替える。
	userAgent := c.userAgent
	if fourChanExternalHosts[host] {
		userAgent = neutralUserAgent
	}
	req.Header.Set("User-Agent", userAgent)

	// 4chan API への再取得は If-Modified-Since を必須とする (初回は送らない)。
	if host == fourChanAPIHost {
		if lm := c.lastModifiedFor(reqURL); lm != "" {
			req.Header.Set("If-Modified-Since", lm)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GETリクエストの送信に失敗しました (%s): %w", reqURL, err)
	}
	defer resp.Body.Close()

	// 304 は 4chan API のみ「更新なし」として返す。ふたば側の処理は変えない。
	if resp.StatusCode == http.StatusNotModified && host == fourChanAPIHost {
		c.recordFetch(reqURL, "", time.Now())
		return "", ErrNotModified
	}

	if resp.StatusCode != http.StatusOK {
		// HTTPErrorとして返す（ステータスコードを含む）
		return "", &HTTPError{
			StatusCode: resp.StatusCode,
			URL:        reqURL,
			Message:    http.StatusText(resp.StatusCode),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("レスポンスボディの読み込みに失敗しました: %w", err)
	}

	if host == fourChanAPIHost {
		c.recordFetch(reqURL, resp.Header.Get("Last-Modified"), time.Now())
	}

	return string(body), nil
}

// getLimiterForHost は、指定されたホスト名に対応するレートリミッターを返します。
// 存在しない場合は新しく生成します。
// この関数は内部でMutexをロックするため、呼び出し側はrateLimitersへの直接アクセスを避けるべきです。
// 特に、この関数で取得したlimiterのWaitメソッドを、rateLimitersMutexのロック内で呼び出すとデッドロックします。
func (c *Client) getLimiterForHost(host string) *rate.Limiter {
	c.rateLimitersMutex.Lock()
	defer c.rateLimitersMutex.Unlock()

	limiterKey := host
	intervalMillis := 1000 // デフォルト1秒

	if host == fourChanAPIHost || host == fourChanCDNHost {
		// a.4cdn.org と i.4cdn.org は合算で 1req/s 以下にする必要があるため、
		// 2 ホストで 1 つのリミッターを共有し、間隔は設定値の最大値 (最低 1 秒) にする。
		limiterKey = fourChanCombinedLimiter
		intervalMillis = int(fourChanMinCombinedInterval / time.Millisecond)
		for _, h := range []string{fourChanAPIHost, fourChanCDNHost} {
			if val, ok := c.perDomainIntervals[h]; ok && val > intervalMillis {
				intervalMillis = val
			}
		}
	} else if val, ok := c.perDomainIntervals[host]; ok && val > 0 {
		intervalMillis = val
	}

	if limiter, exists := c.rateLimiters[limiterKey]; exists {
		return limiter
	}

	// rate.EveryはDurationを受け取るので、ミリ秒をtime.Durationに変換
	limit := rate.Every(time.Duration(intervalMillis) * time.Millisecond)
	newLimiter := rate.NewLimiter(limit, 1) // バーストは1に設定

	c.rateLimiters[limiterKey] = newLimiter
	return newLimiter
}

// lastModifiedFor は、URL に対して保存済みの Last-Modified を返します。
func (c *Client) lastModifiedFor(reqURL string) string {
	c.conditionalMutex.Lock()
	defer c.conditionalMutex.Unlock()
	if e, ok := c.conditional[reqURL]; ok {
		return e.lastModified
	}
	return ""
}

// recordFetch は、URL の最終取得時刻と Last-Modified を記録します。
// lastModified が空の場合は既存値を保持します。
func (c *Client) recordFetch(reqURL string, lastModified string, at time.Time) {
	c.conditionalMutex.Lock()
	defer c.conditionalMutex.Unlock()
	e, ok := c.conditional[reqURL]
	if !ok {
		e = &conditionalEntry{}
		c.conditional[reqURL] = e
	}
	e.lastFetched = at
	if lastModified != "" {
		e.lastModified = lastModified
	}
}

// fourChanThreadWait は、同一スレッドの再取得間隔 (10 秒) を満たすために
// 待つべき時間を返します。対象外の URL は 0 を返します。
func (c *Client) fourChanThreadWait(reqURL string, host string, now time.Time) time.Duration {
	if host != fourChanAPIHost {
		return 0
	}
	u, err := url.Parse(reqURL)
	if err != nil || !fourChanThreadPathPattern.MatchString(u.Path) {
		return 0
	}

	c.conditionalMutex.Lock()
	defer c.conditionalMutex.Unlock()
	e, ok := c.conditional[reqURL]
	if !ok || e.lastFetched.IsZero() {
		return 0
	}
	elapsed := now.Sub(e.lastFetched)
	if elapsed >= fourChanMinThreadInterval {
		return 0
	}
	return fourChanMinThreadInterval - elapsed
}
