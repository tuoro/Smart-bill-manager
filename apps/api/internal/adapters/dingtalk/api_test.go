package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 假钉钉：记录 token 请求次数，校验下载请求的头与 body，按 code 返回不同内容。
func fakeDingTalk(t *testing.T, tokenCalls *int32, payload []byte) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			atomic.AddInt32(tokenCalls, 1)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["appKey"] != "key" || body["appSecret"] != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "tok-" + body["appKey"], "expireIn": 7200})
		case "/v1.0/robot/messageFiles/download":
			if r.Header.Get("x-acs-dingtalk-access-token") != "tok-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["robotCode"] != "key" || body["downloadCode"] == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"downloadUrl": server.URL + "/blob/" + body["downloadCode"]})
		default:
			if strings.HasPrefix(r.URL.Path, "/blob/") {
				_, _ = w.Write(payload)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func newTestAPI(server *httptest.Server, now *time.Time) *OpenAPI {
	api := NewOpenAPI("key", "secret")
	api.base = server.URL
	api.client = server.Client()
	api.now = func() time.Time { return *now }
	return api
}

// token 换一次就够，临近过期才再换：官方接口有频率限制，每条消息都换会被限流。
func TestAccessTokenIsCachedAndRefreshedBeforeExpiry(t *testing.T) {
	var calls int32
	server := fakeDingTalk(t, &calls, []byte("%PDF-1.4 synthetic"))
	now := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	api := newTestAPI(server, &now)
	ctx := context.Background()

	for range 3 {
		if _, err := api.DownloadMessageFile(ctx, "code-1"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("token requested %d times, want 1", calls)
	}
	// 距过期还有 11 分钟：仍用旧 token。
	now = now.Add(7200*time.Second - 11*time.Minute)
	if _, err := api.DownloadMessageFile(ctx, "code-1"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("token refreshed too early: %d", calls)
	}
	// 距过期 9 分钟：提前刷新。
	now = now.Add(2 * time.Minute)
	if _, err := api.DownloadMessageFile(ctx, "code-1"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("token not refreshed near expiry: %d", calls)
	}
}

// 两步下载：先凭 downloadCode + robotCode（=AppKey）换地址，再取内容。
func TestDownloadMessageFileFollowsTheTwoStepProtocol(t *testing.T) {
	var calls int32
	server := fakeDingTalk(t, &calls, []byte("\x89PNG\r\n\x1a\nsynthetic"))
	now := time.Now()
	api := newTestAPI(server, &now)
	content, err := api.DownloadMessageFile(context.Background(), "code-9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "\x89PNG") {
		t.Fatalf("content = %q", content)
	}
}

// 超过 20 MiB 不必下载完，与 documents 表的上限一致。
func TestDownloadMessageFileStopsAtTheSizeLimit(t *testing.T) {
	var calls int32
	server := fakeDingTalk(t, &calls, make([]byte, maxDownloadBytes+1))
	now := time.Now()
	api := newTestAPI(server, &now)
	if _, err := api.DownloadMessageFile(context.Background(), "big"); err != ErrFileTooLarge {
		t.Fatalf("error = %v", err)
	}
}
