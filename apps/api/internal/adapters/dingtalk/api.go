package dingtalk

import (
	"bytes"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatconnectors"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	defaultAPIBase = "https://api.dingtalk.com"
	// 与 documents 表的 size_bytes 上限一致；超过就不必下载完。
	maxDownloadBytes = 20 << 20
	// 官方说 token 有效 7200 秒，提前刷新避免临界失败。
	tokenRefreshMargin = 10 * time.Minute
)

// OpenAPI 只做两件事：换 access token、把 downloadCode 换成文件内容。
// 主动发消息不在这里——这一刀的回执全部走回调自带的 sessionWebhook。
type OpenAPI struct {
	base      string
	appKey    string
	appSecret string
	client    *http.Client
	now       func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func NewOpenAPI(appKey, appSecret string) *OpenAPI {
	return &OpenAPI{
		base:      defaultAPIBase,
		appKey:    appKey,
		appSecret: appSecret,
		client:    &http.Client{Timeout: 30 * time.Second},
		now:       time.Now,
	}
}

// 单飞：并发的两条消息同时发现 token 过期时，只有一个去换，另一个等它。
func (a *OpenAPI) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && a.now().Add(tokenRefreshMargin).Before(a.expiresAt) {
		return a.token, nil
	}
	body, _ := json.Marshal(map[string]string{"appKey": a.appKey, "appSecret": a.appSecret})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/v1.0/oauth2/accessToken", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("request access token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return "", chatconnectors.ErrCredentialsRejected
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request access token: status %d", response.StatusCode)
	}
	var decoded struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int64  `json:"expireIn"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode access token: %w", err)
	}
	if decoded.AccessToken == "" || decoded.ExpireIn <= 0 {
		return "", errors.New("access token response is incomplete")
	}
	a.token = decoded.AccessToken
	a.expiresAt = a.now().Add(time.Duration(decoded.ExpireIn) * time.Second)
	return a.token, nil
}

// DownloadMessageFile 两步：先用 downloadCode 换一个短时下载地址，再取内容。
// robotCode 即应用 AppKey（官方文档如此约定）。
func (a *OpenAPI) DownloadMessageFile(ctx context.Context, downloadCode string) ([]byte, error) {
	token, err := a.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]string{"downloadCode": downloadCode, "robotCode": a.appKey})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/v1.0/robot/messageFiles/download", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-acs-dingtalk-access-token", token)
	response, err := a.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("resolve download url: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resolve download url: status %d", response.StatusCode)
	}
	var decoded struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode download url: %w", err)
	}
	if decoded.DownloadURL == "" {
		return nil, errors.New("download url is empty")
	}
	fileRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, decoded.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	fileResponse, err := a.client.Do(fileRequest)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	defer fileResponse.Body.Close()
	if fileResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download file: status %d", fileResponse.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(fileResponse.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(content) > maxDownloadBytes {
		return nil, ErrFileTooLarge
	}
	return content, nil
}

var ErrFileTooLarge = errors.New("file exceeds the 20 MiB limit")

// Prober 实现 chatconnectors.CredentialProbe：用一对临时凭据换一次 token，不缓存、不建连接。
type Prober struct{ base string }

func NewProber() Prober { return Prober{base: defaultAPIBase} }

func (p Prober) Probe(ctx context.Context, platform, appKey, appSecret string) error {
	api := NewOpenAPI(appKey, appSecret)
	if p.base != "" {
		api.base = p.base
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := api.accessToken(probeCtx)
	return err
}
