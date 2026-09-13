// Package eml parses raw EML messages and extracts headers, bodies, and attachment metadata.
package eml

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"

	"github.com/trustdan/hmba-mail/internal/config"
	"github.com/trustdan/hmba-mail/internal/naming"
	"golang.org/x/text/encoding/htmlindex"
)

// Message represents a parsed email with its metadata and bodies.
type Message struct {
	Raw           []byte
	SHA256        string
	Subject       string
	From          string
	SenderAddress string
	Date          string
	Received      []string
	MessageID     string
	Category      string
	HTMLBody      string
	TextBody      string
	Attachments   []string
}

// WordDecoder decodes RFC 2047 encoded-word headers with charset support.
var wordDecoder = mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		enc, err := htmlindex.Get(charset)
		if err != nil {
			return input, nil
		}
		return enc.NewDecoder().Reader(input), nil
	},
}

// decodeHeader decodes an RFC 2047 encoded header string into clean UTF-8.
func decodeHeader(val string) string {
	decoded, err := wordDecoder.DecodeHeader(val)
	if err != nil {
		return val
	}
	return decoded
}

// Parse decodes raw RFC 822 / MIME bytes and classifies the message according to rules.
func Parse(raw []byte, rules []config.Rule) (*Message, error) {
	sha256Hex := naming.ComputeSHA256(raw)
	r := bytes.NewReader(raw)
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, fmt.Errorf("read email message: %w", err)
	}

	subject := decodeHeader(msg.Header.Get("Subject"))
	fromRaw := decodeHeader(msg.Header.Get("From"))
	senderAddr := ""
	if addr, err := mail.ParseAddress(fromRaw); err == nil {
		senderAddr = addr.Address
	}

	date := msg.Header.Get("Date")
	received := msg.Header["Received"]
	messageID := msg.Header.Get("Message-Id")
	if messageID == "" {
		messageID = msg.Header.Get("Message-ID")
	}

	// Classify based on rules
	category := ""
	for _, rule := range rules {
		if strings.Contains(strings.ToLower(subject), strings.ToLower(rule.Subject)) {
			category = rule.Category
			break
		}
	}

	m := &Message{
		Raw:           raw,
		SHA256:        sha256Hex,
		Subject:       subject,
		From:          fromRaw,
		SenderAddress: senderAddr,
		Date:          date,
		Received:      received,
		MessageID:     messageID,
		Category:      category,
	}

	ct := msg.Header.Get("Content-Type")
	cte := msg.Header.Get("Content-Transfer-Encoding")
	cd := msg.Header.Get("Content-Disposition")

	if err := parseBody(msg.Body, ct, cte, cd, m); err != nil {
		// Non-fatal parse error during body decoding, continue with what we have
		return m, nil
	}

	return m, nil
}

func parseBody(body io.Reader, contentType, transferEncoding, disposition string, m *Message) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
		params = map[string]string{"charset": "utf-8"}
	}

	dispType, dispParams, _ := mime.ParseMediaType(disposition)
	filename := dispParams["filename"]
	if filename == "" {
		filename = params["name"]
	}

	// Check if this part is an attachment
	if dispType == "attachment" || (filename != "" && dispType != "inline") {
		decodedFilename := decodeHeader(filename)
		if decodedFilename == "" {
			decodedFilename = "unnamed attachment"
		}
		m.Attachments = append(m.Attachments, decodedFilename)
		return nil
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return nil
		}
		mr := multipart.NewReader(body, boundary)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			partCT := part.Header.Get("Content-Type")
			partCTE := part.Header.Get("Content-Transfer-Encoding")
			partCD := part.Header.Get("Content-Disposition")
			_ = parseBody(part, partCT, partCTE, partCD, m)
		}
		return nil
	}

	// Leaf part (text/html, text/plain, etc.)
	decodedReader, err := decodeTransferEncoding(body, transferEncoding)
	if err != nil {
		decodedReader = body
	}

	charset := params["charset"]
	if charset == "" {
		charset = "utf-8"
	}
	textReader, err := decodeCharset(decodedReader, charset)
	if err != nil {
		textReader = decodedReader
	}

	contentBytes, err := io.ReadAll(textReader)
	if err != nil {
		return err
	}
	content := string(contentBytes)

	if mediaType == "text/html" {
		if m.HTMLBody == "" {
			m.HTMLBody = content
		} else {
			m.HTMLBody += "\n" + content
		}
	} else if mediaType == "text/plain" {
		if m.TextBody == "" {
			m.TextBody = content
		} else {
			m.TextBody += "\n" + content
		}
	}

	return nil
}

func decodeTransferEncoding(r io.Reader, cte string) (io.Reader, error) {
	cte = strings.ToLower(strings.TrimSpace(cte))
	switch cte {
	case "quoted-printable":
		return quotedprintable.NewReader(r), nil
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, r), nil
	default:
		return r, nil
	}
}

func decodeCharset(r io.Reader, charset string) (io.Reader, error) {
	charset = strings.ToLower(strings.TrimSpace(charset))
	if charset == "utf-8" || charset == "us-ascii" || charset == "" {
		return r, nil
	}
	enc, err := htmlindex.Get(charset)
	if err != nil {
		return r, nil
	}
	return enc.NewDecoder().Reader(r), nil
}

// PreferredBody returns the preferred text content (HTML if available, otherwise Plain text).
func (m *Message) PreferredBody() (content string, isHTML bool) {
	if strings.TrimSpace(m.HTMLBody) != "" {
		return m.HTMLBody, true
	}
	return m.TextBody, false
}
