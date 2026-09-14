package render

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustdan/hmba-mail/internal/config"
	"github.com/trustdan/hmba-mail/internal/eml"
)

func TestConversionPreservesTablesLinksAndImageDescription(t *testing.T) {
	html := `<table><tr><th>Date</th><th>Task</th></tr><tr><td>Sep 11</td><td>Submit</td></tr></table>` +
		`<img src="https://example.test/image" alt="Schedule">` +
		`<img width="1" src="https://example.test/tracker">` +
		`<a href="https://x.safelinks.protection.outlook.com/?url=https%3A%2F%2Fexample.test%2Fa%253Fb%3Fx%3D1%2526y%26z%3D2">Link</a>`

	md := HTMLToMarkdown(html)

	if !strings.Contains(md, "| Date | Task |") {
		t.Errorf("expected table header, got:\n%s", md)
	}
	if !strings.Contains(md, "| Sep 11 | Submit |") {
		t.Errorf("expected table row, got:\n%s", md)
	}
	if !strings.Contains(md, "[Image: Schedule]") {
		t.Errorf("expected Schedule image description, got:\n%s", md)
	}
	if strings.Contains(md, "tracker") {
		t.Errorf("tracker image should be omitted, got:\n%s", md)
	}
	if strings.Contains(md, "![") {
		t.Errorf("Markdown should not contain ![, got:\n%s", md)
	}
	if !strings.Contains(md, "https://example.test/a%3Fb?x=1%26y&z=2") {
		t.Errorf("expected unwrapped link, got:\n%s", md)
	}
}

func TestCanvasLayoutPreservesParagraphsAndNestedDataTable(t *testing.T) {
	html := `<table class="body" style="mso-table-lspace:0pt"><tr><td>` +
		`<table class="main" style="mso-table-lspace:0pt"><tr><td>` +
		`<p>First paragraph</p><p>Second paragraph</p>` +
		`<table><tr><th>Date</th><th>Task</th></tr><tr><td>Sep 11</td><td>Submit</td></tr></table>` +
		`</td></tr></table></td></tr></table>`

	md := HTMLToMarkdown(html)

	if !strings.Contains(md, "First paragraph\n\nSecond paragraph") {
		t.Errorf("expected paragraphs separated by double newline, got:\n%s", md)
	}
	if !strings.Contains(md, "| Date | Task |") {
		t.Errorf("expected nested data table header, got:\n%s", md)
	}
	if strings.Contains(md, "| First paragraph") {
		t.Errorf("First paragraph should not be in a table, got:\n%s", md)
	}
}

func TestRenderFullDocument(t *testing.T) {
	msg := &eml.Message{
		SHA256:        "abc123def456",
		Subject:       "Recent Canvas Notifications",
		From:          "UW Canvas <canvas@example.test>",
		SenderAddress: "canvas@example.test",
		Date:          "Fri, 11 Sep 2026 12:00:00 -0500",
		MessageID:     "<same-id@example.test>",
		Category:      "canvas-digest",
		HTMLBody:      "<p>You have new announcements</p>",
		Attachments:   []string{"schedule.pdf"},
	}

	data, meta, err := Render(msg, "canvas-digest")
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	doc := string(data)
	if !strings.HasPrefix(doc, "---\n") {
		t.Errorf("missing frontmatter start: %s", doc)
	}
	if meta.SenderAddress != "canvas@example.test" {
		t.Errorf("unexpected meta sender: %s", meta.SenderAddress)
	}
	if !strings.Contains(doc, "# Recent Canvas Notifications\n") {
		t.Errorf("missing title heading: %s", doc)
	}
	if !strings.Contains(doc, "You have new announcements") {
		t.Errorf("missing body text: %s", doc)
	}
	if !strings.Contains(doc, "## Attachments\n\nAttachment content remains in the local EML and is not included in this Markdown.\n\n- \"schedule.pdf\"") {
		t.Errorf("missing attachment section: %s", doc)
	}
}

func TestRenderEmptyBodyNote(t *testing.T) {
	msg := &eml.Message{
		SHA256:        "empty123",
		Subject:       "Recent Canvas Notifications",
		From:          "UW Canvas <canvas@example.test>",
		SenderAddress: "canvas@example.test",
		Category:      "canvas-digest",
	}

	data, _, err := Render(msg, "canvas-digest")
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if !bytes.Contains(data, []byte("No readable text body found; inspect the preserved EML.")) {
		t.Errorf("expected empty body note, got:\n%s", string(data))
	}
}

func TestRealEMLConversionFidelity(t *testing.T) {
	fixturePath := filepath.Join("testdata", "sample.eml")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read test fixture: %v", err)
	}

	msg, err := eml.Parse(data, []config.Rule{
		{Subject: "Recent Canvas Notifications", Category: "canvas-digest"},
	})
	if err != nil {
		t.Fatalf("parse test fixture: %v", err)
	}

	rendered, meta, err := Render(msg, "canvas-digest")
	if err != nil {
		t.Fatalf("render test fixture: %v", err)
	}

	if meta.SenderAddress != "canvas@uw.edu" {
		t.Errorf("expected sender canvas@uw.edu, got %s", meta.SenderAddress)
	}
	if !bytes.Contains(rendered, []byte("This Message Is From an Untrusted Sender")) {
		t.Errorf("expected body text in rendered")
	}
	if !bytes.Contains(rendered, []byte(`date: "2026-09-12T09:55:26-05:00"`)) {
		t.Errorf("expected RFC3339 date in frontmatter, got:\n%s", string(rendered[:300]))
	}
	if !bytes.Contains(rendered, []byte(`| Course | Assignment | Due Date |`)) {
		t.Errorf("expected table in rendered Markdown, got:\n%s", string(rendered))
	}
	expectedSource := "originals/" + msg.SHA256 + ".eml"
	if meta.LocalSource != expectedSource {
		t.Errorf("expected portable local source %s, got %s", expectedSource, meta.LocalSource)
	}
}

func TestRenderCustomProvenanceSource(t *testing.T) {
	msg := &eml.Message{
		SHA256:   "hash123",
		Subject:  "Test",
		Category: "canvas-digest",
	}
	data, meta, err := RenderWithSource(msg, "canvas-digest", ".hmba-mail/originals/hash123.eml")
	if err != nil {
		t.Fatalf("RenderWithSource: %v", err)
	}
	if meta.LocalSource != ".hmba-mail/originals/hash123.eml" {
		t.Errorf("expected custom local_source, got %s", meta.LocalSource)
	}
	if !bytes.Contains(data, []byte(`local_source: ".hmba-mail/originals/hash123.eml"`)) {
		t.Errorf("expected custom local_source in frontmatter, got:\n%s", string(data))
	}
}
