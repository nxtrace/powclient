package powclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

var (
	// ErrTooManyRequests is returned when the server responds 429.
	ErrTooManyRequests = errors.New("too many requests")
	// ErrEmptyToken is returned when the server responds success but token is empty.
	ErrEmptyToken = errors.New("empty token from server")
	// ErrInvalidChallenge is returned when the challenge is invalid or cannot be reduced to exactly two prime factors.
	ErrInvalidChallenge = errors.New("invalid challenge integer")

	errNilGetTokenParams = errors.New("nil GetTokenParams")
	errInvalidBaseURL    = errors.New("invalid BaseUrl")

	transportCache sync.Map
)

// HTTPStatusError is returned for non-200 responses (except 429 which maps to ErrTooManyRequests).
type HTTPStatusError struct {
	Code int
	Body string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("http %d: %s", e.Code, e.Body)
}

// bodySnippet reads up to n bytes from r and returns it as string.
// Intended only for error reporting paths.
func bodySnippet(r io.Reader, n int64) string {
	if n <= 0 {
		n = 2048
	}
	b, _ := io.ReadAll(io.LimitReader(r, n))
	return string(b)
}

type Challenge struct {
	RequestID string `json:"request_id"`
	Challenge string `json:"challenge"`
}

type RequestResponse struct {
	Challenge   Challenge `json:"challenge"`
	RequestTime int64     `json:"request_time"`
}

type SubmitRequest struct {
	Challenge   Challenge `json:"challenge"`
	Answer      []string  `json:"answer"`
	RequestTime int64     `json:"request_time"`
}

type SubmitResponse struct {
	Token string `json:"token"`
}

type GetTokenParams struct {
	TimeoutSec  time.Duration
	BaseUrl     string
	RequestPath string
	SubmitPath  string
	UserAgent   string
	SNI         string
	Host        string
	Proxy       *url.URL /** 支持socks5:// http:// **/
}

func NewGetTokenParams() *GetTokenParams {
	return &GetTokenParams{
		TimeoutSec:  5 * time.Second,
		BaseUrl:     "http://127.0.0.1:55000",
		RequestPath: "/request_challenge",
		SubmitPath:  "/submit_answer",
		UserAgent:   "POW client",
		SNI:         "",
		Host:        "",
		Proxy:       nil,
	}
}

type ChallengeParams struct {
	BaseUrl     string
	RequestPath string
	SubmitPath  string
	UserAgent   string
	Host        string
	Client      *http.Client
}

func RetToken(getTokenParams *GetTokenParams) (string, error) {
	if getTokenParams == nil {
		return "", errNilGetTokenParams
	}

	baseURL, err := parseBaseURL(getTokenParams.BaseUrl)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	if getTokenParams.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, getTokenParams.TimeoutSec)
		defer cancel()
	}

	client := &http.Client{
		Transport: getTransport(getTokenParams),
	}

	challengeParams := &ChallengeParams{
		BaseUrl:     getTokenParams.BaseUrl,
		RequestPath: getTokenParams.RequestPath,
		SubmitPath:  getTokenParams.SubmitPath,
		UserAgent:   getTokenParams.UserAgent,
		Host:        getTokenParams.Host,
		Client:      client,
	}

	requestURL := resolveURL(baseURL, getTokenParams.RequestPath)
	submitURL := resolveURL(baseURL, getTokenParams.SubmitPath)

	challengeResponse, err := requestChallenge(ctx, challengeParams, requestURL)
	if err != nil {
		return "", err
	}

	token, err := submitAnswer(ctx, challengeParams, submitURL, challengeResponse)
	if err != nil {
		return "", err
	}

	return token, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errInvalidBaseURL
	}

	baseURL, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidBaseURL, err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("%w: %q", errInvalidBaseURL, raw)
	}
	return baseURL, nil
}

func resolveURL(baseURL *url.URL, endpointPath string) string {
	if endpointPath == "" {
		return baseURL.String()
	}

	resolved := *baseURL
	resolved.Path = path.Join(strings.TrimSuffix(baseURL.Path, "/"), endpointPath)
	return resolved.String()
}

func requestChallenge(ctx context.Context, challengeParams *ChallengeParams, requestURL string) (*RequestResponse, error) {
	var challengeResponse RequestResponse
	if err := doJSONRequest(ctx, challengeParams, http.MethodGet, requestURL, nil, "", &challengeResponse); err != nil {
		return nil, err
	}
	return &challengeResponse, nil
}

func submitAnswer(ctx context.Context, challengeParams *ChallengeParams, submitURL string, challengeResponse *RequestResponse) (string, error) {
	factors, err := solveSemiprime(ctx, challengeResponse.Challenge.Challenge)
	if err != nil {
		return "", err
	}

	submitRequest := SubmitRequest{
		Challenge:   Challenge{RequestID: challengeResponse.Challenge.RequestID},
		Answer:      []string{factors[0].String(), factors[1].String()},
		RequestTime: challengeResponse.RequestTime,
	}
	requestBody, err := json.Marshal(submitRequest)
	if err != nil {
		return "", err
	}

	var submitResponse SubmitResponse
	if err := doJSONRequest(ctx, challengeParams, http.MethodPost, submitURL, bytes.NewReader(requestBody), "application/json", &submitResponse); err != nil {
		return "", err
	}
	if submitResponse.Token == "" {
		return "", ErrEmptyToken
	}
	return submitResponse.Token, nil
}

func doJSONRequest(
	ctx context.Context,
	challengeParams *ChallengeParams,
	method string,
	endpoint string,
	body io.Reader,
	contentType string,
	out any,
) (err error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if challengeParams.UserAgent != "" {
		req.Header.Set("User-Agent", challengeParams.UserAgent)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if challengeParams.Host != "" {
		req.Host = challengeParams.Host
	}

	resp, err := challengeParams.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("close response body: %w", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			return ErrTooManyRequests
		}
		return &HTTPStatusError{Code: resp.StatusCode, Body: bodySnippet(resp.Body, 2048)}
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func getTransport(getTokenParams *GetTokenParams) *http.Transport {
	key := transportCacheKey(getTokenParams)
	if cached, ok := transportCache.Load(key); ok {
		return cached.(*http.Transport)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 128
	transport.MaxIdleConnsPerHost = 32
	if getTokenParams.Proxy != nil {
		transport.Proxy = http.ProxyURL(getTokenParams.Proxy)
	}
	if getTokenParams.SNI != "" {
		tlsConfig := &tls.Config{}
		if transport.TLSClientConfig != nil {
			tlsConfig = transport.TLSClientConfig.Clone()
		}
		tlsConfig.ServerName = getTokenParams.SNI
		transport.TLSClientConfig = tlsConfig
	}

	actual, _ := transportCache.LoadOrStore(key, transport)
	return actual.(*http.Transport)
}

func transportCacheKey(getTokenParams *GetTokenParams) string {
	proxyURL := ""
	if getTokenParams.Proxy != nil {
		proxyURL = getTokenParams.Proxy.String()
	}
	return proxyURL + "\x00" + getTokenParams.SNI
}
