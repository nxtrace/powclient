package powclient

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"net/url"
	"os"
	"testing"
)

func TestGetToken(t *testing.T) {
	token, err := getToken("api.leo.moe", "api.leo.moe", "443")
	fmt.Println(token, err)
	assert.NoError(t, err, "GetToken() returned an error")
}

func getToken(fastIp string, host string, port string) (string, error) {
	var baseURL = "/v3/challenge"
	getTokenParams := NewGetTokenParams()
	u := url.URL{Scheme: "https", Host: fastIp + ":" + port, Path: baseURL}
	getTokenParams.BaseUrl = u.String()
	getTokenParams.SNI = host
	getTokenParams.Host = host
	getTokenParams.Proxy = nil
	var err error
	// 尝试三次RetToken，如果都失败了，异常退出
	for i := 0; i < 3; i++ {
		token, err := RetToken(getTokenParams)
		if err != nil {
			continue
		}
		return token, nil
	}
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("RetToken failed 3 times, exit")
	os.Exit(1)
	return "", nil
}
