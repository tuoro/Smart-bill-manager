package services

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

type FeishuService struct {
	repo           *repository.FeishuRepository
	invoiceService *InvoiceService
	uploadsDir     string

	httpClient *http.Client

	tokenMu     sync.Mutex
	tenantToken string
	tokenExpiry time.Time
}

func NewFeishuService(uploadsDir string, invoiceService *InvoiceService) *FeishuService {
	return &FeishuService{
		repo:           repository.NewFeishuRepository(),
		invoiceService: invoiceService,
		uploadsDir:     uploadsDir,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type CreateFeishuConfigInput struct {
	Name              string  `json:"name" binding:"required"`
	AppID             *string `json:"app_id"`
	AppSecret         *string `json:"app_secret"`
	VerificationToken *string `json:"verification_token"`
	EncryptKey        *string `json:"encrypt_key"`
	IsActive          int     `json:"is_active"`
}

func (s *FeishuService) CreateConfig(input CreateFeishuConfigInput) (*models.FeishuConfig, error) {
	isActive := input.IsActive
	if isActive == 0 {
		isActive = 1
	}

	config := &models.FeishuConfig{
		ID:                utils.GenerateUUID(),
		Name:              input.Name,
		AppID:             input.AppID,
		AppSecret:         input.AppSecret,
		VerificationToken: input.VerificationToken,
		EncryptKey:        input.EncryptKey,
		IsActive:          isActive,
	}

	if err := s.repo.CreateConfig(config); err != nil {
		return nil, err
	}
	return config, nil
}

func (s *FeishuService) GetAllConfigs() ([]models.FeishuConfigResponse, error) {
	configs, err := s.repo.FindAllConfigs()
	if err != nil {
		return nil, err
	}

	out := make([]models.FeishuConfigResponse, 0, len(configs))
	for _, c := range configs {
		out = append(out, c.ToResponse())
	}
	return out, nil
}

func (s *FeishuService) GetConfigByID(id string) (*models.FeishuConfig, error) {
	return s.repo.FindConfigByID(id)
}

func (s *FeishuService) GetActiveConfig() (*models.FeishuConfig, error) {
	return s.repo.FindActiveConfig()
}

func (s *FeishuService) UpdateConfig(id string, data map[string]interface{}) error {
	// Don't update secrets if masked.
	for _, key := range []string{"app_secret", "verification_token", "encrypt_key"} {
		if val, ok := data[key]; ok {
			if val == "********" {
				delete(data, key)
			}
		}
	}
	return s.repo.UpdateConfig(id, data)
}

func (s *FeishuService) DeleteConfig(id string) error {
	return s.repo.DeleteConfig(id)
}

func (s *FeishuService) GetLogs(configID string, limit int) ([]models.FeishuLog, error) {
	if limit == 0 {
		limit = 50
	}
	return s.repo.FindLogs(configID, limit)
}

type feishuURLVerification struct {
	Type      string `json:"type"`
	Token     string `json:"token"`
	Challenge string `json:"challenge"`
}

type feishuEventCallback struct {
	Schema string `json:"schema"`
	Header struct {
		EventType  string `json:"event_type"`
		EventID    string `json:"event_id"`
		CreateTime string `json:"create_time"`
		Token      string `json:"token"`
		TenantKey  string `json:"tenant_key"`
		AppID      string `json:"app_id"`
	} `json:"header"`
	Event json.RawMessage `json:"event"`
}

type feishuMessageReceiveEvent struct {
	Message struct {
		MessageID   string `json:"message_id"`
		ChatID      string `json:"chat_id"`
		ChatType    string `json:"chat_type"`
		MessageType string `json:"message_type"`
		Content     string `json:"content"`
		CreateTime  string `json:"create_time"`
	} `json:"message"`
	Sender struct {
		SenderID struct {
			OpenID  string `json:"open_id"`
			UserID  string `json:"user_id"`
			UnionID string `json:"union_id"`
		} `json:"sender_id"`
		SenderType string `json:"sender_type"`
		TenantKey  string `json:"tenant_key"`
	} `json:"sender"`
}

type feishuEncryptedEnvelope struct {
	Encrypt string `json:"encrypt"`
}

// ProcessWebhookPayload returns challenge when url verification, or nil when processed as event callback.
func (s *FeishuService) ProcessWebhookPayload(ctx context.Context, payload []byte, config *models.FeishuConfig) (*string, error) {
	raw := payload

	var enc feishuEncryptedEnvelope
	if err := json.Unmarshal(payload, &enc); err == nil && enc.Encrypt != "" {
		if config.EncryptKey == nil || strings.TrimSpace(*config.EncryptKey) == "" {
			return nil, fmt.Errorf("encrypted payload received but encrypt_key is not configured")
		}
		plain, err := decryptFeishuEncrypt(*config.EncryptKey, enc.Encrypt)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt payload: %w", err)
		}
		raw = plain
	}

	var uv feishuURLVerification
	if err := json.Unmarshal(raw, &uv); err == nil && uv.Type == "url_verification" && uv.Challenge != "" {
		if !tokenMatches(config.VerificationToken, uv.Token) {
			return nil, fmt.Errorf("verification token mismatch")
		}
		return &uv.Challenge, nil
	}

	var cb feishuEventCallback
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	if cb.Header.EventType == "" {
		return nil, fmt.Errorf("missing header.event_type")
	}
	if cb.Header.Token != "" && !tokenMatches(config.VerificationToken, cb.Header.Token) {
		return nil, fmt.Errorf("verification token mismatch")
	}

	switch cb.Header.EventType {
	case "im.message.receive_v1":
		return nil, s.processMessageReceive(ctx, config, cb.Header.EventType, cb.Event)
	default:
		// Log and ignore unknown event types.
		eventType := cb.Header.EventType
		l := &models.FeishuLog{
			ID:        utils.GenerateUUID(),
			ConfigID:  config.ID,
			EventType: &eventType,
			Status:    "ignored",
		}
		_ = s.repo.CreateLog(l)
		return nil, nil
	}
}

func (s *FeishuService) processMessageReceive(ctx context.Context, config *models.FeishuConfig, eventType string, raw json.RawMessage) error {
	var ev feishuMessageReceiveEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return fmt.Errorf("invalid message event: %w", err)
	}

	msgType := strings.TrimSpace(ev.Message.MessageType)
	content := strings.TrimSpace(ev.Message.Content)

	var (
		hasAttachment int
		fileKey       *string
		fileName      *string
	)

	var logContent *string
	if msgType == "text" {
		var c struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &c); err == nil && strings.TrimSpace(c.Text) != "" {
			t := strings.TrimSpace(c.Text)
			logContent = &t
		}
	}

	switch msgType {
	case "file":
		var c struct {
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
		}
		if err := json.Unmarshal([]byte(content), &c); err == nil && c.FileKey != "" {
			hasAttachment = 1
			fileKey = &c.FileKey
			if strings.TrimSpace(c.FileName) != "" {
				n := strings.TrimSpace(c.FileName)
				fileName = &n
			}
		}
	case "image":
		var c struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(content), &c); err == nil && c.ImageKey != "" {
			hasAttachment = 1
			fileKey = &c.ImageKey
			name := fmt.Sprintf("%s.png", ev.Message.MessageID)
			fileName = &name
		}
	}

	senderID := firstNonEmpty(ev.Sender.SenderID.UserID, ev.Sender.SenderID.OpenID, ev.Sender.SenderID.UnionID)
	chatID := strings.TrimSpace(ev.Message.ChatID)
	messageID := strings.TrimSpace(ev.Message.MessageID)

	status := "processed"
	var errMsg *string

	if hasAttachment == 1 && fileKey != nil && *fileKey != "" && fileName != nil && *fileName != "" {
		resourceType := msgType
		data, err := s.downloadMessageResource(ctx, config, messageID, *fileKey, resourceType)
		if err != nil {
			status = "failed"
			m := err.Error()
			errMsg = &m
		} else {
			savedName, saveErr := s.saveAttachment(*fileName, data)
			if saveErr != nil {
				status = "failed"
				m := saveErr.Error()
				errMsg = &m
			} else {
				if isInvoiceFile(savedName) {
					_, createErr := s.invoiceService.Create(CreateInvoiceInput{
						Filename:     savedName,
						OriginalName: *fileName,
						FilePath:     "uploads/" + savedName,
						FileSize:     int64(len(data)),
						Source:       "feishu",
					})
					if createErr != nil {
						status = "failed"
						m := createErr.Error()
						errMsg = &m
					} else if chatID != "" {
						ack := fmt.Sprintf("已收到文件并导入发票：%s", *fileName)
						if err := s.sendTextToChat(ctx, config, chatID, ack); err != nil {
							log.Printf("[Feishu] send ack failed: %v", err)
						}
					}
				} else {
					status = "skipped"
					if chatID != "" {
						ack := fmt.Sprintf("已收到文件：%s（暂不支持此文件类型）", *fileName)
						if err := s.sendTextToChat(ctx, config, chatID, ack); err != nil {
							log.Printf("[Feishu] send ack failed: %v", err)
						}
					}
				}
			}
		}
	}

	l := &models.FeishuLog{
		ID:            utils.GenerateUUID(),
		ConfigID:      config.ID,
		EventType:     &eventType,
		MessageType:   strPtrOrNil(msgType),
		SenderID:      strPtrOrNil(senderID),
		ChatID:        strPtrOrNil(chatID),
		MessageID:     strPtrOrNil(messageID),
		Content:       logContent,
		FileName:      fileName,
		FileKey:       fileKey,
		HasAttachment: hasAttachment,
		Status:        status,
		Error:         errMsg,
	}
	_ = s.repo.CreateLog(l)
	return nil
}

func (s *FeishuService) saveAttachment(originalName string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext == "" {
		ext = ".bin"
	}

	safe := fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), sanitizeFilenameStrict(strings.TrimSuffix(originalName, filepath.Ext(originalName))), ext)
	if err := os.MkdirAll(s.uploadsDir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(s.uploadsDir, safe), data, 0644); err != nil {
		return "", err
	}
	return safe, nil
}

func isInvoiceFile(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf", ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

func (s *FeishuService) downloadMessageResource(ctx context.Context, config *models.FeishuConfig, messageID, fileKey, resourceType string) ([]byte, error) {
	token, err := s.getTenantAccessToken(ctx, config)
	if err != nil {
		return nil, err
	}

	type tryURL struct {
		url  string
		desc string
	}

	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		resourceType = "file"
	}

	tries := []tryURL{
		{
			url:  fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/resources/%s?type=%s", messageID, fileKey, resourceType),
			desc: "messages resource",
		},
		{
			url:  fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/files/%s?type=%s", fileKey, resourceType),
			desc: "files resource",
		},
	}

	for _, t := range tries {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := s.httpClient.Do(req)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Some failures come back as JSON even with 200; detect common envelope.
			if looksLikeJSON(resp.Header.Get("Content-Type"), body) {
				var apiErr struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				if json.Unmarshal(body, &apiErr) == nil && apiErr.Code != 0 {
					continue
				}
			}
			return body, nil
		}
	}

	return nil, fmt.Errorf("failed to download resource from feishu")
}

func (s *FeishuService) getTenantAccessToken(ctx context.Context, config *models.FeishuConfig) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	if s.tenantToken != "" && time.Now().Before(s.tokenExpiry.Add(-1*time.Minute)) {
		return s.tenantToken, nil
	}

	if config.AppID == nil || config.AppSecret == nil || strings.TrimSpace(*config.AppID) == "" || strings.TrimSpace(*config.AppSecret) == "" {
		return "", fmt.Errorf("feishu config missing app_id/app_secret")
	}

	reqBody, _ := json.Marshal(map[string]string{
		"app_id":     strings.TrimSpace(*config.AppID),
		"app_secret": strings.TrimSpace(*config.AppSecret),
	})

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int64  `json:"expire"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Code != 0 || out.TenantAccessToken == "" {
		if out.Msg == "" {
			out.Msg = string(body)
		}
		return "", fmt.Errorf("feishu token error: %s", out.Msg)
	}

	s.tenantToken = out.TenantAccessToken
	if out.Expire <= 0 {
		out.Expire = 3600
	}
	s.tokenExpiry = time.Now().Add(time.Duration(out.Expire) * time.Second)
	return s.tenantToken, nil
}

func (s *FeishuService) sendTextToChat(ctx context.Context, config *models.FeishuConfig, chatID, text string) error {
	token, err := s.getTenantAccessToken(ctx, config)
	if err != nil {
		return err
	}

	contentJSON, _ := json.Marshal(map[string]string{"text": text})
	reqBody, _ := json.Marshal(map[string]any{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(contentJSON),
	})

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("send message failed: %s", string(body))
	}

	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(body, &apiResp) == nil && apiResp.Code != 0 {
		return fmt.Errorf("send message failed: %s", apiResp.Msg)
	}
	return nil
}

func tokenMatches(configToken *string, incoming string) bool {
	if configToken == nil || strings.TrimSpace(*configToken) == "" {
		// Allow when token is not configured (less secure, but avoids blocking).
		return true
	}
	return strings.TrimSpace(*configToken) == strings.TrimSpace(incoming)
}

func decryptFeishuEncrypt(encryptKey, encryptB64 string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptB64)
	if err != nil {
		return nil, err
	}

	key := []byte(encryptKey)
	if !(len(key) == 16 || len(key) == 24 || len(key) == 32) {
		sum := sha256.Sum256(key)
		key = sum[:]
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid ciphertext length")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, aes.BlockSize)
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)

	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, err
	}

	// Feishu format: 16 bytes random + 4 bytes big endian length + json + app_id
	if len(plain) < 20 {
		return nil, fmt.Errorf("invalid plaintext length")
	}
	jsonLen := binary.BigEndian.Uint32(plain[16:20])
	start := 20
	end := start + int(jsonLen)
	if end > len(plain) || end <= start {
		return nil, fmt.Errorf("invalid json length")
	}
	return plain[start:end], nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padding size")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for i := 0; i < pad; i++ {
		if data[len(data)-1-i] != byte(pad) {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}

func sanitizeFilenameStrict(filename string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "file"
	}
	return re.ReplaceAllString(filename, "_")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func strPtrOrNil(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func looksLikeJSON(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		return true
	}
	b := bytes.TrimSpace(body)
	return len(b) > 0 && (b[0] == '{' || b[0] == '[')
}
