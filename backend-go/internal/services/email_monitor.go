package services

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

type EmailService struct {
	repo              *repository.EmailRepository
	invoiceService    *InvoiceService
	uploadsDir        string
	activeConnections map[string]*client.Client
	mu                sync.RWMutex
}

func NewEmailService(uploadsDir string, invoiceService *InvoiceService) *EmailService {
	return &EmailService{
		repo:              repository.NewEmailRepository(),
		invoiceService:    invoiceService,
		uploadsDir:        uploadsDir,
		activeConnections: make(map[string]*client.Client),
	}
}

type CreateEmailConfigInput struct {
	Email    string `json:"email" binding:"required"`
	IMAPHost string `json:"imap_host" binding:"required"`
	IMAPPort int    `json:"imap_port"`
	Password string `json:"password" binding:"required"`
	IsActive int    `json:"is_active"`
}

func (s *EmailService) CreateConfig(input CreateEmailConfigInput) (*models.EmailConfig, error) {
	port := input.IMAPPort
	if port == 0 {
		port = 993
	}

	isActive := input.IsActive
	if isActive == 0 {
		isActive = 1
	}

	config := &models.EmailConfig{
		ID:       utils.GenerateUUID(),
		Email:    input.Email,
		IMAPHost: input.IMAPHost,
		IMAPPort: port,
		Password: input.Password,
		IsActive: isActive,
	}

	if err := s.repo.CreateConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func (s *EmailService) GetAllConfigs() ([]models.EmailConfigResponse, error) {
	configs, err := s.repo.FindAllConfigs()
	if err != nil {
		return nil, err
	}

	var responses []models.EmailConfigResponse
	for _, c := range configs {
		responses = append(responses, c.ToResponse())
	}
	return responses, nil
}

func (s *EmailService) GetConfigByID(id string) (*models.EmailConfig, error) {
	return s.repo.FindConfigByID(id)
}

func (s *EmailService) UpdateConfig(id string, data map[string]interface{}) error {
	// Don't update password if it's masked
	if pwd, ok := data["password"]; ok {
		if pwd == "********" {
			delete(data, "password")
		}
	}
	return s.repo.UpdateConfig(id, data)
}

func (s *EmailService) DeleteConfig(id string) error {
	s.StopMonitoring(id)
	return s.repo.DeleteConfig(id)
}

func (s *EmailService) GetLogs(configID string, limit int) ([]models.EmailLog, error) {
	if limit == 0 {
		limit = 50
	}
	return s.repo.FindLogs(configID, limit)
}

// TestConnection tests IMAP connection
// Note: InsecureSkipVerify is intentionally set to true to support mail servers
// with self-signed certificates, which is common in enterprise environments.
// This matches the behavior of the original Node.js implementation.
func (s *EmailService) TestConnection(email, imapHost string, imapPort int, password string) (bool, string) {
	addr := fmt.Sprintf("%s:%d", imapHost, imapPort)

	// #nosec G402 - InsecureSkipVerify is intentional to support self-signed certs
	c, err := client.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return false, fmt.Sprintf("连接失败: %v", err)
	}
	defer c.Logout()

	if err := c.Login(email, password); err != nil {
		return false, fmt.Sprintf("登录失败: %v", err)
	}

	return true, "连接成功！"
}

// StartMonitoring starts email monitoring for a config
func (s *EmailService) StartMonitoring(configID string) bool {
	config, err := s.repo.FindConfigByID(configID)
	if err != nil || config.IsActive == 0 {
		return false
	}

	// Stop existing connection
	s.StopMonitoring(configID)

	addr := fmt.Sprintf("%s:%d", config.IMAPHost, config.IMAPPort)
	// #nosec G402 - InsecureSkipVerify is intentional to support self-signed certs
	c, err := client.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("[Email Monitor] Connection error for %s: %v", config.Email, err)
		return false
	}

	if err := c.Login(config.Email, config.Password); err != nil {
		log.Printf("[Email Monitor] Login error for %s: %v", config.Email, err)
		c.Logout()
		return false
	}

	s.mu.Lock()
	s.activeConnections[configID] = c
	s.mu.Unlock()

	log.Printf("[Email Monitor] Connected to %s", config.Email)

	// Start monitoring in goroutine
	go s.monitorInbox(configID, c)

	return true
}

func (s *EmailService) monitorInbox(configID string, c *client.Client) {
	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Printf("[Email Monitor] Error selecting inbox: %v", err)
		s.StopMonitoring(configID)
		return
	}

	log.Printf("[Email Monitor] Inbox opened. %d total messages", mbox.Messages)

	// Fetch unread emails initially
	s.fetchUnreadEmails(configID, c)

	// Set up IDLE for real-time notifications
	updates := make(chan client.Update)
	c.Updates = updates

	done := make(chan error, 1)
	stop := make(chan struct{})

	go func() {
		for {
			select {
			case update := <-updates:
				switch update.(type) {
				case *client.MailboxUpdate:
					log.Println("[Email Monitor] New mail received!")
					s.fetchUnreadEmails(configID, c)
				}
			case <-stop:
				return
			}
		}
	}()

	// Start IDLE
	go func() {
		done <- c.Idle(stop, nil)
	}()

	// Wait for done or connection closed
	select {
	case err := <-done:
		if err != nil {
			log.Printf("[Email Monitor] IDLE error: %v", err)
		}
	}

	close(stop)
	s.mu.Lock()
	delete(s.activeConnections, configID)
	s.mu.Unlock()
}

func (s *EmailService) fetchUnreadEmails(configID string, c *client.Client) {
	// Search for unseen messages
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	uids, err := c.Search(criteria)
	if err != nil {
		log.Printf("[Email Monitor] Search error: %v", err)
		return
	}

	if len(uids) == 0 {
		log.Println("[Email Monitor] No new unread emails")
		return
	}

	log.Printf("[Email Monitor] Found %d unread emails", len(uids))

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids...)

	messages := make(chan *imap.Message, len(uids))
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}

	go func() {
		if err := c.Fetch(seqSet, items, messages); err != nil {
			log.Printf("[Email Monitor] Fetch error: %v", err)
		}
	}()

	for msg := range messages {
		s.processMessage(configID, msg, section)

		// Mark as seen
		seenSet := new(imap.SeqSet)
		seenSet.AddNum(msg.SeqNum)
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		flags := []interface{}{imap.SeenFlag}
		if err := c.Store(seenSet, item, flags, nil); err != nil {
			log.Printf("[Email Monitor] Error marking as seen: %v", err)
		}
	}

	// Update last check time
	now := time.Now().Format(time.RFC3339)
	s.repo.UpdateLastCheck(configID, now)
}

func (s *EmailService) processMessage(configID string, msg *imap.Message, section *imap.BodySectionName) {
	if msg == nil {
		return
	}

	r := msg.GetBody(section)
	if r == nil {
		return
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		log.Printf("[Email Monitor] Error creating mail reader: %v", err)
		return
	}

	var subject, from string
	var receivedDate time.Time

	header := mr.Header
	if s, err := header.Subject(); err == nil {
		subject = s
	}
	if addrs, err := header.AddressList("From"); err == nil && len(addrs) > 0 {
		from = addrs[0].String()
	}
	if d, err := header.Date(); err == nil {
		receivedDate = d
	}

	var attachmentCount int
	var hasAttachment int

	// Process parts
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[Email Monitor] Error reading part: %v", err)
			break
		}

		switch h := p.Header.(type) {
		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			if strings.HasSuffix(strings.ToLower(filename), ".pdf") {
				content, err := io.ReadAll(p.Body)
				if err != nil {
					log.Printf("[Email Monitor] Error reading attachment: %v", err)
					continue
				}
				s.saveAttachment(filename, content, configID)
				attachmentCount++
				hasAttachment = 1
			}
		}
	}

	// Log the email
	dateStr := receivedDate.Format(time.RFC3339)
	if subject == "" {
		subject = "(无主题)"
	}
	emailLog := &models.EmailLog{
		ID:              utils.GenerateUUID(),
		EmailConfigID:   configID,
		Subject:         &subject,
		FromAddress:     &from,
		ReceivedDate:    &dateStr,
		HasAttachment:   hasAttachment,
		AttachmentCount: attachmentCount,
		Status:          "processed",
	}
	s.repo.CreateLog(emailLog)

	log.Printf("[Email Monitor] Email logged: %s", subject)
}

func (s *EmailService) saveAttachment(filename string, content []byte, configID string) {
	safeFilename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeFilename(filename))

	if err := os.MkdirAll(s.uploadsDir, 0755); err != nil {
		log.Printf("[Email Monitor] Error creating uploads dir: %v", err)
		return
	}

	filePath := filepath.Join(s.uploadsDir, safeFilename)
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		log.Printf("[Email Monitor] Error saving attachment: %v", err)
		return
	}

	log.Printf("[Email Monitor] Saved attachment: %s", safeFilename)

	// Create invoice record
	size := int64(len(content))
	_, err := s.invoiceService.Create(CreateInvoiceInput{
		Filename:     safeFilename,
		OriginalName: filename,
		FilePath:     "uploads/" + safeFilename,
		FileSize:     size,
		Source:       "email",
	})
	if err != nil {
		log.Printf("[Email Monitor] Error creating invoice: %v", err)
	}
}

func sanitizeFilename(filename string) string {
	// Replace unsafe characters
	unsafe := []string{"/", "\\", "..", " "}
	result := filename
	for _, u := range unsafe {
		result = strings.ReplaceAll(result, u, "_")
	}
	return result
}

// StopMonitoring stops email monitoring for a config
func (s *EmailService) StopMonitoring(configID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, exists := s.activeConnections[configID]
	if !exists {
		return false
	}

	c.Logout()
	delete(s.activeConnections, configID)
	return true
}

// GetMonitoringStatus returns status of all configs
func (s *EmailService) GetMonitoringStatus() ([]models.MonitorStatus, error) {
	ids, err := s.repo.GetConfigIDs()
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var statuses []models.MonitorStatus
	for _, id := range ids {
		status := "stopped"
		if _, exists := s.activeConnections[id]; exists {
			status = "running"
		}
		statuses = append(statuses, models.MonitorStatus{
			ConfigID: id,
			Status:   status,
		})
	}

	return statuses, nil
}

// ManualCheck performs a manual email check
func (s *EmailService) ManualCheck(configID string) (bool, string, int) {
	config, err := s.repo.FindConfigByID(configID)
	if err != nil {
		return false, "配置不存在", 0
	}

	addr := fmt.Sprintf("%s:%d", config.IMAPHost, config.IMAPPort)
	// #nosec G402 - InsecureSkipVerify is intentional to support self-signed certs
	c, err := client.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return false, fmt.Sprintf("连接失败: %v", err), 0
	}
	defer c.Logout()

	if err := c.Login(config.Email, config.Password); err != nil {
		return false, fmt.Sprintf("登录失败: %v", err), 0
	}

	_, err = c.Select("INBOX", false)
	if err != nil {
		return false, fmt.Sprintf("打开收件箱失败: %v", err), 0
	}

	// Search for unseen messages
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	uids, err := c.Search(criteria)
	if err != nil {
		return false, fmt.Sprintf("搜索邮件失败: %v", err), 0
	}

	if len(uids) == 0 {
		return true, "没有新邮件", 0
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids...)

	messages := make(chan *imap.Message, len(uids))
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}

	go func() {
		if err := c.Fetch(seqSet, items, messages); err != nil {
			log.Printf("[Email Monitor] Fetch error: %v", err)
		}
	}()

	count := 0
	for msg := range messages {
		s.processMessage(configID, msg, section)

		// Mark as seen
		seenSet := new(imap.SeqSet)
		seenSet.AddNum(msg.SeqNum)
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		flags := []interface{}{imap.SeenFlag}
		c.Store(seenSet, item, flags, nil)
		count++
	}

	// Update last check time
	now := time.Now().Format(time.RFC3339)
	s.repo.UpdateLastCheck(configID, now)

	return true, fmt.Sprintf("成功处理 %d 封邮件", count), count
}
