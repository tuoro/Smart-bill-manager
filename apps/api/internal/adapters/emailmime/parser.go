package emailmime

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Parser struct{}

func (Parser) Parse(raw []byte) ports.ParsedEmail {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return blocked("email_mime_invalid", "邮件格式无法解析")
	}
	result := ports.ParsedEmail{
		Subject:       safeHeader(message.Header.Get("Subject"), 500),
		SenderAddress: senderAddress(message.Header.Get("From")),
		SentAt:        sentTime(message.Header.Get("Date")),
		Attachments:   []ports.ParsedEmailAttachment{},
	}
	state := parseState{result: &result}
	if err := state.walk(textproto.MIMEHeader(message.Header), message.Body, 1); err != nil {
		return blocked(parseErrorCode(err), parseErrorText(err))
	}
	return result
}

type parseState struct {
	parts       int
	attachments int
	result      *ports.ParsedEmail
}

func (s *parseState) walk(header textproto.MIMEHeader, body io.Reader, depth int) error {
	if depth > domain.MaxEmailMIMEDepth {
		return errMIMEDepth
	}
	s.parts++
	if s.parts > domain.MaxEmailMIMEParts {
		return errMIMEParts
	}
	mediaType, parameters, err := parseContentType(header.Get("Content-Type"))
	if err != nil {
		return errMIMEInvalid
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := parameters["boundary"]
		if boundary == "" {
			return errMIMEInvalid
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, partErr := reader.NextPart()
			if errors.Is(partErr, io.EOF) {
				return nil
			}
			if partErr != nil {
				return errMIMEInvalid
			}
			walkErr := s.walk(part.Header, part, depth+1)
			_ = part.Close()
			if walkErr != nil {
				return walkErr
			}
		}
	}
	name, disposition, attachment, err := attachmentIdentity(header, parameters)
	if err != nil {
		return errMIMEInvalid
	}
	if !attachment {
		return nil
	}
	s.attachments++
	if s.attachments > domain.MaxEmailAttachments {
		return errAttachmentLimit
	}
	decoded, err := transferDecodedReader(header.Get("Content-Transfer-Encoding"), body)
	if err != nil {
		return errAttachmentDecode
	}
	content, err := io.ReadAll(io.LimitReader(decoded, domain.MaxEmailMessageBytes+1))
	if err != nil || int64(len(content)) > domain.MaxEmailMessageBytes {
		return errAttachmentDecode
	}
	s.result.Attachments = append(s.result.Attachments, ports.ParsedEmailAttachment{
		PartIndex:   s.attachments,
		Name:        safeHeader(name, 1000),
		MIME:        mediaType,
		Disposition: disposition,
		Content:     content,
	})
	return nil
}

func parseContentType(value string) (string, map[string]string, error) {
	if strings.TrimSpace(value) == "" {
		return "text/plain", map[string]string{}, nil
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return "", nil, err
	}
	return strings.ToLower(mediaType), parameters, nil
}

func attachmentIdentity(
	header textproto.MIMEHeader,
	contentTypeParameters map[string]string,
) (string, string, bool, error) {
	disposition := ""
	parameters := map[string]string{}
	if raw := strings.TrimSpace(header.Get("Content-Disposition")); raw != "" {
		parsed, parsedParameters, err := mime.ParseMediaType(raw)
		if err != nil {
			return "", "", false, err
		}
		disposition = strings.ToLower(parsed)
		parameters = parsedParameters
	}
	name := parameters["filename"]
	if name == "" {
		name = contentTypeParameters["name"]
	}
	if decoded, err := new(mime.WordDecoder).DecodeHeader(name); err == nil {
		name = decoded
	}
	attachment := disposition == "attachment" || name != ""
	if !attachment {
		return "", "", false, nil
	}
	if disposition != "inline" {
		disposition = "attachment"
	}
	return name, disposition, true, nil
}

func transferDecodedReader(encoding string, source io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary":
		return source, nil
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, source), nil
	case "quoted-printable":
		return quotedprintable.NewReader(source), nil
	default:
		return nil, errAttachmentDecode
	}
}

func safeHeader(value string, maximum int) string {
	if decoded, err := new(mime.WordDecoder).DecodeHeader(value); err == nil {
		value = decoded
	}
	value = strings.TrimSpace(norm.NFKC.String(value))
	if len([]rune(value)) > maximum {
		return ""
	}
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}

func senderAddress(value string) string {
	parsed, err := mail.ParseAddress(value)
	if err != nil || len(parsed.Address) > 254 {
		return ""
	}
	return strings.ToLower(parsed.Address)
}

func sentTime(value string) *time.Time {
	parsed, err := mail.ParseDate(value)
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

func blocked(code, text string) ports.ParsedEmail {
	return ports.ParsedEmail{
		Attachments:   []ports.ParsedEmailAttachment{},
		BlockedCode:   code,
		SafeErrorText: text,
	}
}

var (
	errMIMEInvalid      = errors.New("invalid MIME")
	errMIMEDepth        = errors.New("MIME depth exceeded")
	errMIMEParts        = errors.New("MIME part limit exceeded")
	errAttachmentLimit  = errors.New("attachment limit exceeded")
	errAttachmentDecode = errors.New("attachment decode failed")
)

func parseErrorCode(err error) string {
	switch {
	case errors.Is(err, errMIMEDepth):
		return "email_mime_depth_exceeded"
	case errors.Is(err, errMIMEParts):
		return "email_mime_part_limit_exceeded"
	case errors.Is(err, errAttachmentLimit):
		return "email_attachment_limit_exceeded"
	case errors.Is(err, errAttachmentDecode):
		return "email_attachment_decode_failed"
	default:
		return "email_mime_invalid"
	}
}

func parseErrorText(err error) string {
	switch parseErrorCode(err) {
	case "email_mime_depth_exceeded":
		return "邮件 MIME 嵌套超过 10 层"
	case "email_mime_part_limit_exceeded":
		return "邮件 MIME part 超过 200 个"
	case "email_attachment_limit_exceeded":
		return "邮件附件超过 50 个"
	case "email_attachment_decode_failed":
		return "邮件附件传输编码无法安全解码"
	default:
		return "邮件格式无法解析"
	}
}
