package powclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRetTokenSuccess(t *testing.T) {
	var (
		mu         sync.Mutex
		gotHosts   []string
		gotAnswers []string
		gotAgent   string
		gotReqID   string
		gotReqTime int64
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pow/request_challenge":
			mu.Lock()
			gotHosts = append(gotHosts, r.Host)
			gotAgent = r.UserAgent()
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(RequestResponse{
				Challenge: Challenge{
					RequestID: "req-1",
					Challenge: "35",
				},
				RequestTime: 123,
			})
		case "/pow/submit_answer":
			mu.Lock()
			gotHosts = append(gotHosts, r.Host)
			mu.Unlock()

			var submitRequest SubmitRequest
			if err := json.NewDecoder(r.Body).Decode(&submitRequest); err != nil {
				t.Fatalf("decode submit request: %v", err)
			}

			mu.Lock()
			gotAnswers = append([]string(nil), submitRequest.Answer...)
			gotReqID = submitRequest.Challenge.RequestID
			gotReqTime = submitRequest.RequestTime
			mu.Unlock()

			_ = json.NewEncoder(w).Encode(SubmitResponse{Token: "token-123"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	params := NewGetTokenParams()
	params.BaseUrl = server.URL + "/pow"
	params.Host = "pow.example"
	params.UserAgent = "powclient-test"

	token, err := RetToken(params)
	if err != nil {
		t.Fatalf("RetToken() error = %v", err)
	}
	if token != "token-123" {
		t.Fatalf("RetToken() token = %q, want %q", token, "token-123")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(gotHosts) != 2 {
		t.Fatalf("host observations = %d, want 2", len(gotHosts))
	}
	for i, gotHost := range gotHosts {
		if gotHost != "pow.example" {
			t.Fatalf("host[%d] = %q, want %q", i, gotHost, "pow.example")
		}
	}
	if gotAgent != "powclient-test" {
		t.Fatalf("user agent = %q, want %q", gotAgent, "powclient-test")
	}
	if gotReqID != "req-1" {
		t.Fatalf("request id = %q, want %q", gotReqID, "req-1")
	}
	if gotReqTime != 123 {
		t.Fatalf("request time = %d, want 123", gotReqTime)
	}
	if strings.Join(gotAnswers, ",") != "5,7" {
		t.Fatalf("answers = %v, want [5 7]", gotAnswers)
	}
}

func TestRetTokenNilParams(t *testing.T) {
	_, err := RetToken(nil)
	if !errors.Is(err, errNilGetTokenParams) {
		t.Fatalf("RetToken(nil) error = %v, want %v", err, errNilGetTokenParams)
	}
}

func TestRetTokenInvalidBaseURL(t *testing.T) {
	params := NewGetTokenParams()
	params.BaseUrl = "://bad-url"

	_, err := RetToken(params)
	if !errors.Is(err, errInvalidBaseURL) {
		t.Fatalf("RetToken() invalid BaseUrl error = %v, want %v", err, errInvalidBaseURL)
	}
}

func TestRetTokenTooManyRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()

	params := NewGetTokenParams()
	params.BaseUrl = server.URL

	_, err := RetToken(params)
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("RetToken() error = %v, want %v", err, ErrTooManyRequests)
	}
}

func TestRetTokenHTTPStatusErrorIncludesSnippet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("x", 4096), http.StatusInternalServerError)
	}))
	defer server.Close()

	params := NewGetTokenParams()
	params.BaseUrl = server.URL

	_, err := RetToken(params)
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("RetToken() error = %v, want *HTTPStatusError", err)
	}
	if httpErr.Code != http.StatusInternalServerError {
		t.Fatalf("HTTPStatusError.Code = %d, want %d", httpErr.Code, http.StatusInternalServerError)
	}
	if len(httpErr.Body) != 2048 {
		t.Fatalf("HTTPStatusError.Body len = %d, want 2048", len(httpErr.Body))
	}
}

func TestRetTokenEmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/request_challenge":
			_ = json.NewEncoder(w).Encode(RequestResponse{
				Challenge:   Challenge{RequestID: "req-1", Challenge: "35"},
				RequestTime: 123,
			})
		case "/submit_answer":
			_ = json.NewEncoder(w).Encode(SubmitResponse{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	params := NewGetTokenParams()
	params.BaseUrl = server.URL

	_, err := RetToken(params)
	if !errors.Is(err, ErrEmptyToken) {
		t.Fatalf("RetToken() error = %v, want %v", err, ErrEmptyToken)
	}
}

func TestRetTokenInvalidChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/request_challenge":
			_ = json.NewEncoder(w).Encode(RequestResponse{
				Challenge:   Challenge{RequestID: "req-1", Challenge: "13"},
				RequestTime: 123,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	params := NewGetTokenParams()
	params.BaseUrl = server.URL

	_, err := RetToken(params)
	if !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("RetToken() error = %v, want %v", err, ErrInvalidChallenge)
	}
}

func TestRetTokenInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{not-json")
	}))
	defer server.Close()

	params := NewGetTokenParams()
	params.BaseUrl = server.URL

	_, err := RetToken(params)
	if err == nil {
		t.Fatal("RetToken() error = nil, want JSON decode error")
	}
}

func TestRetTokenTimeoutDuringSolve(t *testing.T) {
	originalHook := pollardRhoStepHook
	pollardRhoStepHook = func() {
		time.Sleep(2 * time.Millisecond)
	}
	defer func() {
		pollardRhoStepHook = originalHook
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/request_challenge":
			_ = json.NewEncoder(w).Encode(RequestResponse{
				Challenge:   Challenge{RequestID: "req-1", Challenge: "849387260465695603243"},
				RequestTime: 123,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	params := NewGetTokenParams()
	params.BaseUrl = server.URL
	params.TimeoutSec = time.Millisecond

	_, err := RetToken(params)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RetToken() timeout error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestTransportReuseAndIsolation(t *testing.T) {
	proxyOne, err := url.Parse("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("parse proxy one: %v", err)
	}
	proxyTwo, err := url.Parse("http://127.0.0.1:8081")
	if err != nil {
		t.Fatalf("parse proxy two: %v", err)
	}

	base := NewGetTokenParams()
	base.SNI = "pow.example"
	base.Proxy = proxyOne

	same := NewGetTokenParams()
	same.SNI = "pow.example"
	same.Proxy = proxyOne

	differentSNI := NewGetTokenParams()
	differentSNI.SNI = "other.example"
	differentSNI.Proxy = proxyOne

	differentProxy := NewGetTokenParams()
	differentProxy.SNI = "pow.example"
	differentProxy.Proxy = proxyTwo

	tr1 := getTransport(base)
	tr2 := getTransport(same)
	if tr1 != tr2 {
		t.Fatal("getTransport() did not reuse transport for identical proxy/SNI")
	}
	if tr1.TLSClientConfig == nil || tr1.TLSClientConfig.ServerName != "pow.example" {
		t.Fatalf("TLS ServerName = %v, want %q", tr1.TLSClientConfig, "pow.example")
	}

	proxyURL, err := tr1.Proxy(&http.Request{URL: mustParseURL(t, "https://example.com")})
	if err != nil {
		t.Fatalf("transport proxy lookup: %v", err)
	}
	if proxyURL.String() != proxyOne.String() {
		t.Fatalf("proxy URL = %q, want %q", proxyURL.String(), proxyOne.String())
	}

	if getTransport(differentSNI) == tr1 {
		t.Fatal("different SNI unexpectedly reused transport")
	}
	if getTransport(differentProxy) == tr1 {
		t.Fatal("different proxy unexpectedly reused transport")
	}
}

func TestRetTokenReusesHTTPConnection(t *testing.T) {
	var (
		mu          sync.Mutex
		remoteAddrs = map[string]struct{}{}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		remoteAddrs[r.RemoteAddr] = struct{}{}
		mu.Unlock()

		switch r.URL.Path {
		case "/request_challenge":
			_ = json.NewEncoder(w).Encode(RequestResponse{
				Challenge:   Challenge{RequestID: "req-1", Challenge: "35"},
				RequestTime: 123,
			})
		case "/submit_answer":
			_ = json.NewEncoder(w).Encode(SubmitResponse{Token: "token-123"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	params := NewGetTokenParams()
	params.BaseUrl = server.URL

	for i := 0; i < 2; i++ {
		token, err := RetToken(params)
		if err != nil {
			t.Fatalf("RetToken() call %d error = %v", i, err)
		}
		if token == "" {
			t.Fatalf("RetToken() call %d returned empty token", i)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(remoteAddrs) != 1 {
		t.Fatalf("unique remote connections = %d, want 1", len(remoteAddrs))
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsedURL, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL %q: %v", raw, err)
	}
	return parsedURL
}
