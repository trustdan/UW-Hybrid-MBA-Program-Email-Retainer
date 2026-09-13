package eml

import (
	"strings"
	"testing"

	"github.com/trustdan/hmba-mail/internal/config"
)

var defaultRules = []config.Rule{
	{Subject: "Recent Canvas Notifications", Category: "canvas-digest"},
	{Subject: "Weekly Announcement", Category: "program-announcement"},
}

func TestParseSimplePlainEmail(t *testing.T) {
	raw := []byte("From: UW Canvas <canvas@example.test>\r\n" +
		"Subject: Recent Canvas Notifications\r\n" +
		"Date: Fri, 11 Sep 2026 12:00:00 -0500\r\n" +
		"Message-ID: <same-id@example.test>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello Canvas Digest Body\r\n")

	msg, err := Parse(raw, defaultRules)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if msg.Category != "canvas-digest" {
		t.Errorf("expected canvas-digest, got %s", msg.Category)
	}
	if msg.SenderAddress != "canvas@example.test" {
		t.Errorf("expected canvas@example.test, got %s", msg.SenderAddress)
	}
	if !strings.Contains(msg.TextBody, "Hello Canvas Digest Body") {
		t.Errorf("unexpected text body: %s", msg.TextBody)
	}
	if len(msg.Attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(msg.Attachments))
	}
}

func TestRepliesAndForwardsFromOtherSenders(t *testing.T) {
	cases := []struct {
		subject  string
		category string
	}{
		{"Re: recent CANVAS notifications", "canvas-digest"},
		{"Fwd: **Weekly Announcement** Sep 11", "program-announcement"},
		{"Course Director Reply: RE: Recent Canvas Notifications", "canvas-digest"},
		{"Unrelated Newsletter", ""},
	}

	for _, c := range cases {
		raw := []byte("From: Course Director <director@example.test>\r\n" +
			"Subject: " + c.subject + "\r\n" +
			"Date: Sat, 12 Sep 2026 09:00:00 -0700\r\n" +
			"\r\n" +
			"Reply content\r\n")

		msg, err := Parse(raw, defaultRules)
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}
		if msg.Category != c.category {
			t.Errorf("subject %q: expected category %q, got %q", c.subject, c.category, msg.Category)
		}
		if msg.SenderAddress != "director@example.test" {
			t.Errorf("expected director@example.test, got %q", msg.SenderAddress)
		}
	}
}

func TestMultipartWithAttachment(t *testing.T) {
	boundary := "----=_Part_12345"
	raw := []byte("From: UW Canvas <canvas@example.test>\r\n" +
		"Subject: Recent Canvas Notifications\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>Notification details</p>\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: application/pdf; name=\"schedule.pdf\"\r\n" +
		"Content-Disposition: attachment; filename=\"schedule.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"c29tZSBwZGYgYnl0ZXM=\r\n" +
		"--" + boundary + "--\r\n")

	msg, err := Parse(raw, defaultRules)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(msg.Attachments) != 1 || msg.Attachments[0] != "schedule.pdf" {
		t.Fatalf("expected attachment schedule.pdf, got %v", msg.Attachments)
	}
	body, isHTML := msg.PreferredBody()
	if !isHTML || !strings.Contains(body, "Notification details") {
		t.Fatalf("expected HTML body, got: %s", body)
	}
}

func TestEncodedHeadersAndQuotedPrintable(t *testing.T) {
	// =?UTF-8?B?V2Vla2x5IEFubm91bmNlbWVudA==?= -> Weekly Announcement
	raw := []byte("From: =?UTF-8?B?SHlicmlkIE1CQQ==?= <hmba@example.test>\r\n" +
		"Subject: =?UTF-8?B?V2Vla2x5IEFubm91bmNlbWVudA==?= Update\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Here is the update with smart quotes: =E2=80=9CHello=E2=80=9D=0A")

	msg, err := Parse(raw, defaultRules)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if msg.Category != "program-announcement" {
		t.Errorf("expected program-announcement, got %s", msg.Category)
	}
	if !strings.Contains(msg.Subject, "Weekly Announcement Update") {
		t.Errorf("expected decoded subject, got %s", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "“Hello”") {
		t.Errorf("expected decoded quoted-printable with smart quotes, got %s", msg.TextBody)
	}
}
