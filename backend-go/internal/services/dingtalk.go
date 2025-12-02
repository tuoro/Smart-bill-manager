package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

type DingtalkService struct {
	repo           *repository.DingtalkRepository
	invoiceService *InvoiceService
	uploadsDir     string
}

func NewDingtalkService(uploadsDir string, invoiceService *InvoiceService) *DingtalkService {
	return &DingtalkService{
		repo:           repository.NewDingtalkRepository(),
		invoiceService: invoiceService,
		uploadsDir:     uploadsDir,
	}
}

type CreateDingtalkConfigInput struct {
	Name         string  `json:"name" binding:"required"`
	AppKey       *string `json:"app_key"`
	AppSecret    *string `json:"app_secret"`
	WebhookToken *string `json:"webhook_token"`
	IsActive     int     `json:"is_active"`
}

func (s *DingtalkService) CreateConfig(input CreateDingtalkConfigInput) (*models.DingtalkConfig, error) {
	isActive := input.IsActive
	if isActive == 0 {
		isActive = 1
	}

	config := &models.DingtalkConfig{
		ID:           utils.GenerateUUID(),
		Name:         input.Name,
		AppKey:       input.AppKey,
		AppSecret:    input.AppSecret,
		WebhookToken: input.WebhookToken,
		IsActive:     isActive,
	}

	if err := s.repo.CreateConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func (s *DingtalkService) GetAllConfigs() ([]models.DingtalkConfigResponse, error) {
	configs, err := s.repo.FindAllConfigs()
	if err != nil {
		return nil, err
	}

	var responses []models.DingtalkConfigResponse
	for _, c := range configs {
		responses = append(responses, c.ToResponse())
	}
	return responses, nil
}

func (s *DingtalkService) GetConfigByID(id string) (*models.DingtalkConfig, error) {
	return s.repo.FindConfigByID(id)
}

func (s *DingtalkService) GetActiveConfig() (*models.DingtalkConfig, error) {
	return s.repo.FindActiveConfig()
}

func (s *DingtalkService) UpdateConfig(id string, data map[string]interface{}) error {
	// Don't update secrets if masked
	for _, key := range []string{"app_secret", "webhook_token"} {
		if val, ok := data[key]; ok {
			if val == "********" {
				delete(data, key)
			}
		}
	}
	return s.repo.UpdateConfig(id, data)
}

func (s *DingtalkService) DeleteConfig(id string) error {
	return s.repo.DeleteConfig(id)
}

func (s *DingtalkService) GetLogs(configID string, limit int) ([]models.DingtalkLog, error) {
	if limit == 0 {
		limit = 50
	}
	return s.repo.FindLogs(configID, limit)
}

// VerifySignature verifies DingTalk webhook signature
func (s *DingtalkService) VerifySignature(timestamp, sign, secret string) bool {
	if timestamp == "" || sign == "" || secret == "" {
		return false
	}

	stringToSign := fmt.Sprintf("%s\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	calculatedSign := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return calculatedSign == sign
}

// ProcessWebhookMessage processes incoming DingTalk message
func (s *DingtalkService) ProcessWebhookMessage(message *models.DingtalkMessage, configID string) (*models.DingtalkResponse, string, error) {
	var attachmentCount int
	var hasAttachment int
	var content string

	messageType := message.Msgtype
	if messageType == "" {
		messageType = "unknown"
	}

	if message.Text != nil {
		content = message.Text.Content
	}

	// Check for file type messages
	if message.Msgtype == "file" && message.Content != nil && message.Content.DownloadCode != "" {
		hasAttachment = 1
		attachmentCount = 1

		// Download and process the file
		err := s.downloadAndProcessFile(message.Content.DownloadCode, message.Content.FileName, configID)
		if err != nil {
			log.Printf("[DingTalk] Error downloading file: %v", err)
			content = fmt.Sprintf("文件下载失败: %s", message.Content.FileName)
		} else {
			content = fmt.Sprintf("文件: %s", message.Content.FileName)
		}
	}

	// Log the message
	log := &models.DingtalkLog{
		ID:              utils.GenerateUUID(),
		ConfigID:        configID,
		MessageType:     &messageType,
		SenderNick:      &message.SenderNick,
		SenderID:        &message.SenderID,
		Content:         &content,
		HasAttachment:   hasAttachment,
		AttachmentCount: attachmentCount,
		Status:          "processed",
	}
	s.repo.CreateLog(log)

	// Prepare response
	responseContent := "收到消息"
	if hasAttachment > 0 {
		responseContent = "收到发票文件，正在处理中..."
	} else if content != "" {
		if len(content) > 50 {
			responseContent = fmt.Sprintf("收到消息: %s...", content[:50])
		} else {
			responseContent = fmt.Sprintf("收到消息: %s", content)
		}
	}

	response := &models.DingtalkResponse{
		Msgtype: "text",
		Text: models.DingtalkTextReply{
			Content: responseContent,
		},
	}

	return response, "消息处理成功", nil
}

func (s *DingtalkService) downloadAndProcessFile(downloadCode, fileName, configID string) error {
	config, err := s.repo.FindConfigByID(configID)
	if err != nil {
		return fmt.Errorf("config not found: %w", err)
	}

	if config.AppKey == nil || config.AppSecret == nil || *config.AppKey == "" || *config.AppSecret == "" {
		return fmt.Errorf("DingTalk configuration missing app_key or app_secret")
	}

	// Get access token
	accessToken, err := s.getAccessToken(*config.AppKey, *config.AppSecret)
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	// Download file
	fileBuffer, err := s.downloadFileWithToken(downloadCode, accessToken)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}

	// Save file
	return s.saveFile(fileBuffer, fileName, configID)
}

func (s *DingtalkService) getAccessToken(appKey, appSecret string) (string, error) {
	apiURL := fmt.Sprintf("https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s",
		url.QueryEscape(appKey), url.QueryEscape(appSecret))

	resp, err := http.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("%s", result.ErrMsg)
	}

	return result.AccessToken, nil
}

func (s *DingtalkService) downloadFileWithToken(downloadCode, accessToken string) ([]byte, error) {
	apiURL := fmt.Sprintf("https://oapi.dingtalk.com/robot/message/file/download?access_token=%s", accessToken)

	reqBody, _ := json.Marshal(map[string]string{
		"downloadCode": downloadCode,
		"robotCode":    "",
	})

	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Check if it's an error response
	var errResp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.ErrCode != 0 {
		return nil, fmt.Errorf("%s", errResp.ErrMsg)
	}

	return body, nil
}

func (s *DingtalkService) saveFile(fileBuffer []byte, fileName, configID string) error {
	// Sanitize filename
	safeFileName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), s.sanitizeFilename(fileName))

	if err := os.MkdirAll(s.uploadsDir, 0755); err != nil {
		return err
	}

	filePath := filepath.Join(s.uploadsDir, safeFileName)
	if err := os.WriteFile(filePath, fileBuffer, 0644); err != nil {
		return err
	}

	log.Printf("[DingTalk] Saved file: %s", safeFileName)

	// Only create invoice if PDF
	if strings.HasSuffix(strings.ToLower(fileName), ".pdf") {
		size := int64(len(fileBuffer))
		_, err := s.invoiceService.Create(CreateInvoiceInput{
			Filename:     safeFileName,
			OriginalName: fileName,
			FilePath:     "uploads/" + safeFileName,
			FileSize:     size,
			Source:       "dingtalk",
		})
		if err != nil {
			log.Printf("[DingTalk] Error creating invoice: %v", err)
		}
	}

	return nil
}

func (s *DingtalkService) sanitizeFilename(filename string) string {
	// Replace unsafe characters
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	return re.ReplaceAllString(filename, "_")
}

// DownloadFromURL downloads file from URL
func (s *DingtalkService) DownloadFromURL(fileURL, fileName, configID string) error {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(fileURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Handle redirect
	if resp.StatusCode == 301 || resp.StatusCode == 302 {
		location := resp.Header.Get("Location")
		if location != "" {
			return s.DownloadFromURL(location, fileName, configID)
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return s.saveFile(body, fileName, configID)
}

// SendResponse sends response to DingTalk session webhook
func (s *DingtalkService) SendResponse(sessionWebhook string, response *models.DingtalkResponse) error {
	reqBody, err := json.Marshal(response)
	if err != nil {
		return err
	}

	resp, err := http.Post(sessionWebhook, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[DingTalk] Response sent: %s", string(body))

	return nil
}
