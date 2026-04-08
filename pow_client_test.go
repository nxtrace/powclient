package powclient

import (
	"net/url"
	"os"
	"testing"
)

func TestRetTokenIntegration(t *testing.T) {
	baseURL := os.Getenv("POWCLIENT_INTEGRATION_BASE_URL")
	if baseURL == "" {
		t.Skip("set POWCLIENT_INTEGRATION_BASE_URL to run the live integration test")
	}

	params := NewGetTokenParams()
	params.BaseUrl = baseURL
	if requestPath := os.Getenv("POWCLIENT_INTEGRATION_REQUEST_PATH"); requestPath != "" {
		params.RequestPath = requestPath
	}
	if submitPath := os.Getenv("POWCLIENT_INTEGRATION_SUBMIT_PATH"); submitPath != "" {
		params.SubmitPath = submitPath
	}
	if userAgent := os.Getenv("POWCLIENT_INTEGRATION_USER_AGENT"); userAgent != "" {
		params.UserAgent = userAgent
	}
	if sni := os.Getenv("POWCLIENT_INTEGRATION_SNI"); sni != "" {
		params.SNI = sni
	}
	if host := os.Getenv("POWCLIENT_INTEGRATION_HOST"); host != "" {
		params.Host = host
	}
	if proxyValue := os.Getenv("POWCLIENT_INTEGRATION_PROXY"); proxyValue != "" {
		proxyURL, err := url.Parse(proxyValue)
		if err != nil {
			t.Fatalf("parse POWCLIENT_INTEGRATION_PROXY: %v", err)
		}
		params.Proxy = proxyURL
	}

	token, err := RetToken(params)
	if err != nil {
		t.Fatalf("RetToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("RetToken() returned an empty token")
	}
}
