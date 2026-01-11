package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
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

func formatIMAPLoginError(imapHost string, err error) string {
	base := ""
	if err != nil {
		base = err.Error()
	}
	host := strings.ToLower(strings.TrimSpace(imapHost))
	if host == "" {
		return base
	}

	// QQ Mail commonly requires enabling IMAP/SMTP and using an authorization code (not the login password).
	if strings.Contains(host, "qq.com") {
		lower := strings.ToLower(base)
		if strings.Contains(lower, "login fail") || strings.Contains(lower, "authentication failed") || strings.Contains(lower, "auth") {
			return fmt.Sprintf(
				"%s\n\n提示：QQ邮箱需要在网页版「设置 -> 账户」开启 IMAP/SMTP 服务，并使用生成的“授权码”（不是QQ登录密码）。如果提示登录频率限制/账号异常，建议稍后重试或先网页登录确认账号状态。",
				base,
			)
		}
	}

	return base
}

type EmailService struct {
	repo              *repository.EmailRepository
	invoiceService    *InvoiceService
	uploadsDir        string
	activeConnections map[string]*client.Client
	mu                sync.RWMutex
}

func formatIMAPAddress(a *imap.Address) string {
	if a == nil {
		return ""
	}
	addr := strings.TrimSpace(a.Address())
	name := strings.TrimSpace(a.PersonalName)
	if name == "" {
		return addr
	}
	return fmt.Sprintf("%s <%s>", name, addr)
}

// countInvoiceAttachments counts attachments in a message body structure.
// It is used for UI display ("附件 X 个") during mailbox sync where we only have BODYSTRUCTURE.
func countInvoiceAttachments(bs *imap.BodyStructure) (hasAttachment int, attachmentCount int) {
	if bs == nil {
		return 0, 0
	}

	extractFilename := func(part *imap.BodyStructure) string {
		if part == nil {
			return ""
		}
		// Prefer the library helper; it handles common Param/DispositionParam variants.
		if v, err := part.Filename(); err == nil {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
		// Fallback: case-insensitive lookup across Params/DispositionParams.
		if part.DispositionParams != nil {
			for k, v := range part.DispositionParams {
				if strings.EqualFold(strings.TrimSpace(k), "filename") || strings.EqualFold(strings.TrimSpace(k), "name") {
					if s := strings.TrimSpace(v); s != "" {
						return s
					}
				}
			}
		}
		if part.Params != nil {
			for k, v := range part.Params {
				if strings.EqualFold(strings.TrimSpace(k), "name") || strings.EqualFold(strings.TrimSpace(k), "filename") {
					if s := strings.TrimSpace(v); s != "" {
						return s
					}
				}
			}
		}
		return ""
	}

	var walk func(part *imap.BodyStructure)
	walk = func(part *imap.BodyStructure) {
		if part == nil {
			return
		}
		if strings.EqualFold(part.MIMEType, "multipart") {
			for _, child := range part.Parts {
				walk(child)
			}
			return
		}
		if strings.EqualFold(part.MIMEType, "message") && strings.EqualFold(part.MIMESubType, "rfc822") && part.BodyStructure != nil {
			walk(part.BodyStructure)
			return
		}

		filename := strings.TrimSpace(extractFilename(part))
		mimeType := strings.ToLower(strings.TrimSpace(part.MIMEType))
		mimeSubType := strings.ToLower(strings.TrimSpace(part.MIMESubType))
		mime := strings.TrimSpace(mimeType + "/" + mimeSubType)
		disp := strings.ToLower(strings.TrimSpace(part.Disposition))

		// Skip typical body parts (text/plain, text/html) unless they explicitly look like attachments.
		if filename == "" && disp != "attachment" && mimeType == "text" && (mimeSubType == "plain" || mimeSubType == "html") {
			return
		}

		// Primary signals for attachments.
		isAttachment := disp == "attachment" || filename != ""

		// Fallback: some servers omit filename/disposition; treat obvious invoice formats as attachments.
		if !isAttachment {
			if mime == "application/pdf" || strings.Contains(mime, "xml") {
				isAttachment = true
			}
		}

		if !isAttachment {
			// As a last resort, count non-text leaf parts as attachments if they have any identifier.
			// (Avoids undercounting when servers strip filename but keep Content-Id/Description.)
			if mimeType != "multipart" && mimeType != "text" && (strings.TrimSpace(part.Id) != "" || strings.TrimSpace(part.Description) != "") {
				isAttachment = true
			}
		}

		if isAttachment {
			attachmentCount++
			hasAttachment = 1
		}
	}

	walk(bs)
	return hasAttachment, attachmentCount
}

// Backward-compatible alias for older tests/callers.
func countPDFAttachments(bs *imap.BodyStructure) (hasAttachment int, attachmentCount int) {
	return countInvoiceAttachments(bs)
}

func readIMAPBodyWithLimit(r io.Reader, maxBytes int64) (string, error) {
	if r == nil || maxBytes <= 0 {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(r, maxBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

var urlRegex = regexp.MustCompile(`https?://[^\s"'<>]+`)

func extractInvoiceLinksFromText(body string) (xmlURL *string, pdfURL *string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil
	}

	// Decode common HTML escaping from text parts.
	body = strings.ReplaceAll(body, "&amp;", "&")

	urls := urlRegex.FindAllString(body, -1)
	if len(urls) == 0 {
		return nil, nil
	}

	cleanURL := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimRight(s, ">)].,;\"'")
		return s
	}

	for _, raw := range urls {
		u := cleanURL(raw)
		if u == "" {
			continue
		}
		l := strings.ToLower(u)
		if xmlURL == nil {
			if strings.Contains(l, ".xml") || strings.Contains(l, "xml") {
				xmlURL = ptrString(u)
			}
		}
		if pdfURL == nil {
			if strings.Contains(l, ".pdf") || strings.Contains(l, "pdf") {
				pdfURL = ptrString(u)
			}
		}
		if xmlURL != nil && pdfURL != nil {
			return xmlURL, pdfURL
		}
	}

	return xmlURL, pdfURL
}

func ptrString(s string) *string {
	v := strings.TrimSpace(s)
	if v == "" {
		return nil
	}
	return &v
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

func (s *EmailService) CreateConfig(ownerUserID string, input CreateEmailConfigInput) (*models.EmailConfig, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, fmt.Errorf("missing owner_user_id")
	}
	port := input.IMAPPort
	if port == 0 {
		port = 993
	}

	isActive := input.IsActive
	if isActive == 0 {
		isActive = 1
	}

	config := &models.EmailConfig{
		ID:          utils.GenerateUUID(),
		OwnerUserID: ownerUserID,
		Email:       input.Email,
		IMAPHost:    input.IMAPHost,
		IMAPPort:    port,
		Password:    input.Password,
		IsActive:    isActive,
	}

	if err := s.repo.CreateConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func (s *EmailService) GetAllConfigs(ownerUserID string) ([]models.EmailConfigResponse, error) {
	return s.GetAllConfigsCtx(context.Background(), ownerUserID)
}

func (s *EmailService) GetAllConfigsCtx(ctx context.Context, ownerUserID string) ([]models.EmailConfigResponse, error) {
	configs, err := s.repo.FindAllConfigsCtx(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}

	var responses []models.EmailConfigResponse
	for _, c := range configs {
		responses = append(responses, c.ToResponse())
	}
	return responses, nil
}

func (s *EmailService) GetConfigByID(ownerUserID string, id string) (*models.EmailConfig, error) {
	return s.GetConfigByIDCtx(context.Background(), ownerUserID, id)
}

func (s *EmailService) GetConfigByIDCtx(ctx context.Context, ownerUserID string, id string) (*models.EmailConfig, error) {
	return s.repo.FindConfigByIDForOwnerCtx(ctx, ownerUserID, id)
}

func (s *EmailService) UpdateConfig(ownerUserID string, id string, data map[string]interface{}) error {
	// Don't update password if it's masked
	if pwd, ok := data["password"]; ok {
		if pwd == "********" {
			delete(data, "password")
		}
	}
	return s.repo.UpdateConfigForOwner(ownerUserID, id, data)
}

func (s *EmailService) DeleteConfig(ownerUserID string, id string) error {
	if _, err := s.repo.FindConfigByIDForOwner(ownerUserID, id); err != nil {
		return err
	}
	s.StopMonitoring(id)
	return s.repo.DeleteConfigForOwnerCascade(ownerUserID, id)
}

func (s *EmailService) GetLogs(ownerUserID string, configID string, limit int) ([]models.EmailLog, error) {
	return s.GetLogsCtx(context.Background(), ownerUserID, configID, limit)
}

func (s *EmailService) GetLogsCtx(ctx context.Context, ownerUserID string, configID string, limit int) ([]models.EmailLog, error) {
	if limit == 0 {
		limit = 50
	}
	if strings.TrimSpace(configID) != "" {
		if _, err := s.repo.FindConfigByIDForOwnerCtx(ctx, ownerUserID, configID); err != nil {
			return []models.EmailLog{}, nil
		}
	}
	return s.repo.FindLogsCtx(ctx, ownerUserID, configID, limit)
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
		return false, fmt.Sprintf("登录失败: %s", formatIMAPLoginError(imapHost, err))
	}

	return true, "连接成功！"
}

// StartMonitoring starts email monitoring for a config
func (s *EmailService) StartMonitoring(ownerUserID string, configID string) bool {
	config, err := s.repo.FindConfigByIDForOwner(ownerUserID, configID)
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
	needFullSync := config.LastCheck == nil || strings.TrimSpace(*config.LastCheck) == ""
	go s.monitorInbox(configID, strings.TrimSpace(config.OwnerUserID), needFullSync, c)

	return true
}

func (s *EmailService) monitorInbox(configID string, ownerUserID string, fullSync bool, c *client.Client) {
	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Printf("[Email Monitor] Error selecting inbox: %v", err)
		s.StopMonitoring(configID)
		return
	}

	log.Printf("[Email Monitor] Inbox opened. %d total messages", mbox.Messages)

	// On first run (no last_check), do a one-time historical sync.
	if fullSync {
		if _, err := s.fetchEmails(ownerUserID, configID, c, true); err != nil {
			log.Printf("[Email Monitor] Full sync error: %v", err)
		}
	} else {
		if _, err := s.fetchEmails(ownerUserID, configID, c, false); err != nil {
			log.Printf("[Email Monitor] Fetch error: %v", err)
		}
	}

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
					if _, err := s.fetchEmails(ownerUserID, configID, c, false); err != nil {
						log.Printf("[Email Monitor] Fetch error: %v", err)
					}
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
	if err := <-done; err != nil {
		log.Printf("[Email Monitor] IDLE error: %v", err)
	}

	close(stop)
	s.mu.Lock()
	delete(s.activeConnections, configID)
	s.mu.Unlock()
}

func (s *EmailService) fetchEmails(ownerUserID string, configID string, c *client.Client, full bool) (int, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	if ownerUserID == "" || configID == "" || c == nil {
		return 0, fmt.Errorf("missing fields")
	}

	// Only fetch metadata. The detailed email body is read later during "解析" to avoid
	// slow/fragile full-sync behavior on some IMAP servers.
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchBodyStructure}

	newLogs := 0
	fetchChunk := func(seqSet *imap.SeqSet) error {
		if seqSet == nil {
			return fmt.Errorf("nil seqSet")
		}
		messages := make(chan *imap.Message, 32)
		errCh := make(chan error, 1)
		go func() {
			errCh <- c.UidFetch(seqSet, items, messages)
		}()
		for msg := range messages {
			if s.processMessage(ownerUserID, configID, msg, nil) {
				newLogs++
			}
			if !full && msg != nil {
				s.markSeenByUID(c, msg.Uid)
			}
		}
		return <-errCh
	}

	if full {
		// Some IMAP servers behave unexpectedly with "UID FETCH 1:*" for large mailboxes.
		// Use a two-step approach: UID SEARCH ALL -> chunked UID FETCH.
		criteria := imap.NewSearchCriteria() // defaults to ALL
		uids, err := c.UidSearch(criteria)
		if err != nil {
			log.Printf("[Email Monitor] Full sync UidSearch error: %v (falling back to UID FETCH 1:*)", err)
			seqSet := new(imap.SeqSet)
			seqSet.AddRange(1, 0) // 1:* (all UIDs)
			if err := fetchChunk(seqSet); err != nil {
				log.Printf("[Email Monitor] UidFetch error: %v", err)
				return newLogs, err
			}
		} else {
			if len(uids) == 0 {
				return 0, nil
			}
			log.Printf("[Email Monitor] Full sync: %d messages (uid list size=%d)", len(uids), len(uids))

			const chunkSize = 50
			for i := 0; i < len(uids); i += chunkSize {
				end := i + chunkSize
				if end > len(uids) {
					end = len(uids)
				}
				seqSet := new(imap.SeqSet)
				seqSet.AddNum(uids[i:end]...)
				if err := fetchChunk(seqSet); err != nil {
					log.Printf("[Email Monitor] UidFetch error: %v", err)
					return newLogs, err
				}
			}
		}
	} else {
		criteria := imap.NewSearchCriteria()
		criteria.WithoutFlags = []string{imap.SeenFlag}

		uids, err := c.UidSearch(criteria)
		if err != nil {
			log.Printf("[Email Monitor] Search error: %v", err)
			return 0, err
		}
		if len(uids) == 0 {
			return 0, nil
		}

		const chunkSize = 50
		for i := 0; i < len(uids); i += chunkSize {
			end := i + chunkSize
			if end > len(uids) {
				end = len(uids)
			}
			seqSet := new(imap.SeqSet)
			seqSet.AddNum(uids[i:end]...)
			if err := fetchChunk(seqSet); err != nil {
				log.Printf("[Email Monitor] UidFetch error: %v", err)
				return newLogs, err
			}
		}
	}

	now := time.Now().Format(time.RFC3339)
	_ = s.repo.UpdateLastCheckForOwner(ownerUserID, configID, now)
	return newLogs, nil
}

func (s *EmailService) markSeenByUID(c *client.Client, uid uint32) {
	if c == nil || uid == 0 {
		return
	}
	seenSet := new(imap.SeqSet)
	seenSet.AddNum(uid)
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.SeenFlag}
	if err := c.UidStore(seenSet, item, flags, nil); err != nil {
		log.Printf("[Email Monitor] Error marking as seen: %v", err)
	}
}

func (s *EmailService) processMessage(ownerUserID string, configID string, msg *imap.Message, section *imap.BodySectionName) bool {
	if msg == nil {
		return false
	}
	if msg.Uid == 0 {
		return false
	}

	// If the log already exists, still backfill attachment/link metadata (older builds may have missed it).
	if existing, err := s.repo.FindLogByUIDCtx(context.Background(), ownerUserID, configID, "INBOX", msg.Uid); err == nil && existing != nil {
		hasAttachment, attachmentCount := countInvoiceAttachments(msg.BodyStructure)

		var bodyText string
		if section != nil {
			if r := msg.GetBody(section); r != nil {
				if s, err := readIMAPBodyWithLimit(r, 256*1024); err == nil {
					bodyText = s
				}
			}
		}
		xmlURL, pdfURL := extractInvoiceLinksFromText(bodyText)

		updates := map[string]interface{}{}
		if hasAttachment > existing.HasAttachment {
			updates["has_attachment"] = hasAttachment
		}
		if attachmentCount > existing.AttachmentCount {
			updates["attachment_count"] = attachmentCount
		}
		if existing.InvoiceXMLURL == nil && xmlURL != nil && strings.TrimSpace(*xmlURL) != "" {
			updates["invoice_xml_url"] = strings.TrimSpace(*xmlURL)
		}
		if existing.InvoicePDFURL == nil && pdfURL != nil && strings.TrimSpace(*pdfURL) != "" {
			updates["invoice_pdf_url"] = strings.TrimSpace(*pdfURL)
		}
		if len(updates) > 0 {
			_ = s.repo.UpdateLog(existing.ID, updates)
		}
		return false
	}

	// New behavior: only log metadata + (optional) URLs, do not download PDF attachments or create invoices automatically.
	envelope := msg.Envelope
	if envelope != nil {
		subject := strings.TrimSpace(envelope.Subject)
		if subject == "" {
			subject = "(无主题)"
		}

		from := ""
		if len(envelope.From) > 0 {
			from = formatIMAPAddress(envelope.From[0])
		}

		receivedDate := envelope.Date
		dateStr := receivedDate.Format(time.RFC3339)

		hasAttachment, attachmentCount := countInvoiceAttachments(msg.BodyStructure)

		var bodyText string
		if section != nil {
			if r := msg.GetBody(section); r != nil {
				if s, err := readIMAPBodyWithLimit(r, 256*1024); err == nil {
					bodyText = s
				}
			}
		}
		xmlURL, pdfURL := extractInvoiceLinksFromText(bodyText)

		emailLog := &models.EmailLog{
			ID:              utils.GenerateUUID(),
			OwnerUserID:     strings.TrimSpace(ownerUserID),
			EmailConfigID:   configID,
			Mailbox:         "INBOX",
			MessageUID:      msg.Uid,
			Subject:         &subject,
			FromAddress:     &from,
			ReceivedDate:    &dateStr,
			HasAttachment:   hasAttachment,
			AttachmentCount: attachmentCount,
			InvoiceXMLURL:   xmlURL,
			InvoicePDFURL:   pdfURL,
			Status:          "received",
		}
		_ = s.repo.CreateLog(emailLog)

		log.Printf("[Email Monitor] Email logged: %s", subject)
		return true
	}

	r := msg.GetBody(section)
	if r == nil {
		return false
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		log.Printf("[Email Monitor] Error creating mail reader: %v", err)
		return false
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
				content, err := readWithLimit(p.Body, emailParseMaxPDFBytes)
				if err != nil {
					log.Printf("[Email Monitor] Error reading attachment: %v", err)
					continue
				}
				s.saveAttachment(ownerUserID, filename, content)
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
		OwnerUserID:     strings.TrimSpace(ownerUserID),
		EmailConfigID:   configID,
		Mailbox:         "INBOX",
		MessageUID:      msg.Uid,
		Subject:         &subject,
		FromAddress:     &from,
		ReceivedDate:    &dateStr,
		HasAttachment:   hasAttachment,
		AttachmentCount: attachmentCount,
		Status:          "processed",
	}
	s.repo.CreateLog(emailLog)

	log.Printf("[Email Monitor] Email logged: %s", subject)
	return true
}

func (s *EmailService) saveAttachment(ownerUserID string, filename string, content []byte) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	safeFilename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeFilename(filename))

	targetDir := s.uploadsDir
	if ownerUserID != "" {
		targetDir = filepath.Join(s.uploadsDir, ownerUserID)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Printf("[Email Monitor] Error creating uploads dir: %v", err)
		return
	}

	filePath := filepath.Join(targetDir, safeFilename)
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		log.Printf("[Email Monitor] Error saving attachment: %v", err)
		return
	}

	log.Printf("[Email Monitor] Saved attachment: %s", safeFilename)
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
func (s *EmailService) GetMonitoringStatus(ownerUserID string) ([]models.MonitorStatus, error) {
	return s.GetMonitoringStatusCtx(context.Background(), ownerUserID)
}

func (s *EmailService) GetMonitoringStatusCtx(ctx context.Context, ownerUserID string) ([]models.MonitorStatus, error) {
	ids, err := s.repo.GetConfigIDsCtx(ctx, ownerUserID)
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
func (s *EmailService) ManualCheck(ownerUserID string, configID string, full bool) (bool, string, int) {
	config, err := s.repo.FindConfigByIDForOwner(ownerUserID, configID)
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
		return false, fmt.Sprintf("登录失败: %s", formatIMAPLoginError(config.IMAPHost, err)), 0
	}

	_, err = c.Select("INBOX", false)
	if err != nil {
		return false, fmt.Sprintf("打开收件箱失败: %v", err), 0
	}

	count, fetchErr := s.fetchEmails(ownerUserID, configID, c, full)
	if fetchErr != nil {
		if full {
			return false, fmt.Sprintf("全量同步失败: %v", fetchErr), 0
		}
		return false, fmt.Sprintf("同步失败: %v", fetchErr), 0
	}

	// Update last check time
	now := time.Now().Format(time.RFC3339)
	_ = s.repo.UpdateLastCheckForOwner(ownerUserID, configID, now)

	if count == 0 {
		return true, "没有新邮件", 0
	}
	if full {
		return true, fmt.Sprintf("成功同步 %d 封邮件", count), count
	}
	return true, fmt.Sprintf("成功处理 %d 封邮件", count), count
}
