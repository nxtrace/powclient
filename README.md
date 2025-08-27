<div align="center">

<img src="https://github.com/sjlleo/nexttrace/raw/main/asset/logo.png" height="200px" alt="NextTrace Logo"/>

</div>

# POW CLIENT

NEXTTRACE项目派生的仓库，用于实现POW反爬

client : https://github.com/tsosunchia/powclient

server : https://github.com/tsosunchia/powserver

## 导出类型与函数
```go
// 获取 token 的主入口。
// 成功返回 (token, nil)；失败返回 ("", err)。
func RetToken(p *GetTokenParams) (string, error)

// 参数：
type GetTokenParams struct {
    TimeoutSec  time.Duration // 整体请求超时
    BaseUrl     string        // 例如 "https://example.com"
    RequestPath string        // 例如 "/request_challenge"
    SubmitPath  string        // 例如 "/submit_answer"
    UserAgent   string
    SNI         string        // 可选：自定义 TLS SNI；不填则用默认的主机名
    Host        string        // 可选：仅当非空时设置 req.Host
    Proxy       *url.URL      // 可选：socks5:// 或 http://。不设置时默认走环境代理
}

func NewGetTokenParams() *GetTokenParams // 提供一份可用的默认值
```

## 错误模型（可用于精细化处理）
```go
var (
    ErrTooManyRequests  = errors.New("too many requests")      // 429
    ErrEmptyToken       = errors.New("empty token from server") // 200 但 token 为空
    ErrInvalidChallenge = errors.New("invalid challenge integer")
)

type HTTPStatusError struct {
    Code int    // HTTP 状态码（非 200 且非 429）
    Body string // 响应体前 2KB 片段（用于排错）
}
```  
- 发生 429：返回 ErrTooManyRequests（可做重试/退避）
- 发生其它非 200：返回 *HTTPStatusError（可通过 errors.As 拿到 Code/Body）
- 发生 200 但 token 为空：返回 ErrEmptyToken
- 挑战串解析失败：返回 ErrInvalidChallenge（包含原始字符串）

## DEMO
```go
package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nxtrace/powclient"
)

func main() {
	p := powclient.NewGetTokenParams()
	p.BaseUrl = "https://pow.example.com"
	p.TimeoutSec = 5 * time.Second

	token, err := powclient.RetToken(p)
	if err != nil {
		// 统一错误处理
		if errors.Is(err, powclient.ErrTooManyRequests) {
			log.Println("rate limited, please retry later")
			return
		}
		var he *powclient.HTTPStatusError
		if errors.As(err, &he) {
			log.Printf("http error: code=%d body=%q\n", he.Code, he.Body)
			return
		}
		log.Printf("get token failed: %v\n", err)
		return
	}

	fmt.Println("token:", token)
}
```
