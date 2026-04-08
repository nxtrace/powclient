package powclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const benchmarkLargeChallenge = "849387260465695603243"

func BenchmarkSolveSemiprime(b *testing.B) {
	benchmarks := []struct {
		name      string
		challenge string
		parallel  bool
		wantErr   error
	}{
		{name: "small_semiprime_serial", challenge: "35"},
		{name: "small_semiprime_parallel", challenge: "35", parallel: true},
		{name: "large_semiprime_serial", challenge: benchmarkLargeChallenge},
		{name: "large_semiprime_parallel", challenge: benchmarkLargeChallenge, parallel: true},
		{name: "invalid_prime_parallel", challenge: "13", parallel: true, wantErr: ErrInvalidChallenge},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()

			call := func() error {
				_, err := solveSemiprime(context.Background(), bm.challenge)
				if bm.wantErr != nil {
					if !errors.Is(err, bm.wantErr) {
						return fmt.Errorf("solveSemiprime(%q) error = %v, want %v", bm.challenge, err, bm.wantErr)
					}
					return nil
				}
				if err != nil {
					return fmt.Errorf("solveSemiprime(%q): %w", bm.challenge, err)
				}
				return nil
			}

			if bm.parallel {
				runParallelBenchmark(b, 0, call)
				return
			}

			for i := 0; i < b.N; i++ {
				if err := call(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRetToken(b *testing.B) {
	benchmarks := []struct {
		name         string
		challenge    string
		parallel     bool
		isolated     bool
		timeout      time.Duration
		wantErr      error
		hookDelay    time.Duration
		requestDelay time.Duration
	}{
		{name: "reused_transport_serial", challenge: "35"},
		{name: "reused_transport_parallel", challenge: "35", parallel: true, requestDelay: 500 * time.Microsecond},
		{name: "isolated_transport_parallel", challenge: "35", parallel: true, isolated: true, requestDelay: 500 * time.Microsecond},
		{name: "timeout_path_parallel", challenge: benchmarkLargeChallenge, parallel: true, timeout: time.Millisecond, wantErr: context.DeadlineExceeded, hookDelay: 2 * time.Millisecond},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			clearTransportCache()
			server, requests := newBenchmarkPowServer(bm.challenge, bm.requestDelay)
			b.Cleanup(func() {
				server.Close()
				clearTransportCache()
			})

			if bm.hookDelay > 0 {
				originalHook := pollardRhoStepHook
				pollardRhoStepHook = func() {
					time.Sleep(bm.hookDelay)
				}
				b.Cleanup(func() {
					pollardRhoStepHook = originalHook
				})
			}

			var serialCounter uint64
			call := func() error {
				params := newBenchmarkGetTokenParams(server.URL, bm.timeout)
				if bm.isolated {
					params.SNI = fmt.Sprintf("bench-%d", atomic.AddUint64(&serialCounter, 1))
				}

				_, err := RetToken(params)
				if bm.isolated {
					key := transportCacheKey(params)
					if value, ok := transportCache.Load(key); ok {
						if transport, ok := value.(*http.Transport); ok {
							transport.CloseIdleConnections()
						}
						transportCache.Delete(key)
					}
				}
				if bm.wantErr != nil {
					if !errors.Is(err, bm.wantErr) {
						return fmt.Errorf("RetToken() error = %v, want %v", err, bm.wantErr)
					}
					return nil
				}
				if err != nil {
					return fmt.Errorf("RetToken(): %w", err)
				}
				return nil
			}

			b.ReportAllocs()
			if bm.parallel {
				b.SetParallelism(4)
				runParallelBenchmark(b, 2, call)
			} else {
				for i := 0; i < b.N; i++ {
					if err := call(); err != nil {
						b.Fatal(err)
					}
				}
			}

			if requests.Load() == 0 {
				b.Fatal("benchmark server did not receive any requests")
			}
		})
	}
}

func newBenchmarkPowServer(challenge string, requestDelay time.Duration) (*httptest.Server, *atomic.Int64) {
	requests := &atomic.Int64{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if requestDelay > 0 {
			time.Sleep(requestDelay)
		}

		switch r.URL.Path {
		case "/request_challenge":
			_ = json.NewEncoder(w).Encode(RequestResponse{
				Challenge:   Challenge{RequestID: "bench-req", Challenge: challenge},
				RequestTime: 123,
			})
		case "/submit_answer":
			_ = json.NewEncoder(w).Encode(SubmitResponse{Token: "token-123"})
		default:
			http.NotFound(w, r)
		}
	}))

	return server, requests
}

func newBenchmarkGetTokenParams(baseURL string, timeout time.Duration) *GetTokenParams {
	params := NewGetTokenParams()
	params.BaseUrl = baseURL
	params.TimeoutSec = timeout
	return params
}

func runParallelBenchmark(b *testing.B, maxConcurrent int, fn func() error) {
	b.Helper()

	var (
		once     sync.Once
		firstErr error
	)

	var limiter chan struct{}
	if maxConcurrent > 0 {
		limiter = make(chan struct{}, maxConcurrent)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if limiter != nil {
				limiter <- struct{}{}
			}

			err := fn()

			if limiter != nil {
				<-limiter
			}
			if err != nil {
				once.Do(func() {
					firstErr = err
				})
			}
		}
	})

	if firstErr != nil {
		b.Fatal(firstErr)
	}
}

func clearTransportCache() {
	transportCache.Range(func(key, value any) bool {
		if transport, ok := value.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
		transportCache.Delete(key)
		return true
	})
}
