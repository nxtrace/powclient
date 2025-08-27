package powclient

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"time"
)

var (
	// ErrTooManyRequests is returned when the server responds 429.
	ErrTooManyRequests = errors.New("too many requests")
	// ErrEmptyToken is returned when the server responds success but token is empty.
	ErrEmptyToken = errors.New("empty token from server")
	// ErrInvalidChallenge is returned when the challenge string cannot be parsed as a base-10 big.Int.
	ErrInvalidChallenge = errors.New("invalid challenge integer")
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
		TimeoutSec:  5 * time.Second, // 你的默认值
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
	// Build transport by cloning the default so we inherit sane defaults
	tr := http.DefaultTransport.(*http.Transport).Clone()

	// Keep environment proxy unless user explicitly passes one
	if getTokenParams.Proxy != nil {
		tr.Proxy = http.ProxyURL(getTokenParams.Proxy)
	}

	// Apply custom SNI if provided
	if getTokenParams.SNI != "" {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.ServerName = getTokenParams.SNI
	}

	client := &http.Client{
		Timeout:   getTokenParams.TimeoutSec,
		Transport: tr,
	}

	challengeParams := &ChallengeParams{
		BaseUrl:     getTokenParams.BaseUrl,
		RequestPath: getTokenParams.RequestPath,
		SubmitPath:  getTokenParams.SubmitPath,
		UserAgent:   getTokenParams.UserAgent,
		Host:        getTokenParams.Host,
		Client:      client,
	}
	// Get challenge
	challengeResponse, err := requestChallenge(challengeParams)
	if err != nil {
		return "", err
	}

	// Solve challenge and submit answer
	token, err := submitAnswer(challengeParams, challengeResponse)
	if err != nil {
		return "", err
	}

	return token, nil
}

func requestChallenge(challengeParams *ChallengeParams) (*RequestResponse, error) {
	req, err := http.NewRequest("GET", challengeParams.BaseUrl+challengeParams.RequestPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", challengeParams.UserAgent)
	//req.Header.Add("Host", getTokenParams.Host)
	if challengeParams.Host != "" {
		req.Host = challengeParams.Host
	}
	resp, err := challengeParams.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, ErrTooManyRequests
		}
		snippet := bodySnippet(resp.Body, 2048)
		return nil, &HTTPStatusError{Code: resp.StatusCode, Body: snippet}
	}

	var challengeResponse RequestResponse
	err = json.NewDecoder(resp.Body).Decode(&challengeResponse)
	if err != nil {
		return nil, err
	}

	return &challengeResponse, nil
}

func submitAnswer(challengeParams *ChallengeParams, challengeResponse *RequestResponse) (string, error) {
	requestTime := challengeResponse.RequestTime
	challenge := challengeResponse.Challenge.Challenge
	requestId := challengeResponse.Challenge.RequestID
	N, ok := new(big.Int).SetString(challenge, 10)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidChallenge, challenge)
	}
	factorsList := factors(N)
	if len(factorsList) != 2 {
		return "", errors.New("factors function did not return exactly two factors")
	}
	p1 := factorsList[0]
	p2 := factorsList[1]
	if p1.Cmp(p2) > 0 { // if p1 > p2
		p1, p2 = p2, p1 // swap p1 and p2
	}
	submitRequest := SubmitRequest{
		Challenge:   Challenge{RequestID: requestId},
		Answer:      []string{p1.String(), p2.String()},
		RequestTime: requestTime,
	}
	requestBody, err := json.Marshal(submitRequest)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", challengeParams.BaseUrl+challengeParams.SubmitPath, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("User-Agent", challengeParams.UserAgent)
	//req.Header.Add("Host", getTokenParams.Host)
	if challengeParams.Host != "" {
		req.Host = challengeParams.Host
	}

	resp, err := challengeParams.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			return "", ErrTooManyRequests
		}
		snippet := bodySnippet(resp.Body, 2048)
		return "", &HTTPStatusError{Code: resp.StatusCode, Body: snippet}
	}

	var submitResponse SubmitResponse
	err = json.NewDecoder(resp.Body).Decode(&submitResponse)
	if err != nil {
		return "", err
	}

	if submitResponse.Token == "" {
		return "", ErrEmptyToken
	}

	return submitResponse.Token, nil
}
