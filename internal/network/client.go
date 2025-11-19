// Package network は、GIBAのHTTP通信に関する機能を提供します。
// Cookie Jarによるセッション管理をカプセル化した、より高レベルな
// HTTPクライアントを実装しています。
package network

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"GoImageBoardArchiver/internal/config"

	"golang.org/x/time/rate"
)

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

	// ドメインごとのレートリミッターを取得し、待機
	host := parsedURL.Hostname()
	limiter := c.getLimiterForHost(host)

	// 排他制御を追加
	c.rateLimitersMutex.Lock()
	defer c.rateLimitersMutex.Unlock()

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
	// User-Agentも設定
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GETリクエストの送信に失敗しました (%s): %w", reqURL, err)
	}
	defer resp.Body.Close()

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

	return string(body), nil
}

// getLimiterForHost は、指定されたホスト名に対応するレートリミッターを返します。
// 存在しない場合は新しく生成します。
func (c *Client) getLimiterForHost(host string) *rate.Limiter {
	c.rateLimitersMutex.Lock()
	defer c.rateLimitersMutex.Unlock()

	if limiter, exists := c.rateLimiters[host]; exists {
		return limiter
	}

	// 設定された間隔、またはデフォルトの1000ms間隔で新しいリミッターを生成
	intervalMillis := 1000 // デフォルト1秒
	if val, ok := c.perDomainIntervals[host]; ok && val > 0 {
		intervalMillis = val
	}

	// rate.EveryはDurationを受け取るので、ミリ秒をtime.Durationに変換
	limit := rate.Every(time.Duration(intervalMillis) * time.Millisecond)
	newLimiter := rate.NewLimiter(limit, 1) // バーストは1に設定

	c.rateLimiters[host] = newLimiter
	return newLimiter
}
