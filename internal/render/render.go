// Package render converts parsed emails into deterministic UTF-8 Markdown with YAML frontmatter.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/trustdan/hmba-mail/internal/eml"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	reMultipleNewlines = regexp.MustCompile(`\n{3,}`)
	reWhitespace       = regexp.MustCompile(`[ \t]+`)
)

// Metadata contains frontmatter fields for the generated Markdown document.
type Metadata struct {
	Title         string   `json:"title"`
	Date          *string  `json:"date"`
	From          string   `json:"from"`
	SenderAddress string   `json:"sender_address"`
	Category      string   `json:"category"`
	MessageID     string   `json:"message_id"`
	SourceSHA256  string   `json:"source_sha256"`
	LocalSource   string   `json:"local_source"`
	Attachments   []string `json:"attachments"`
}

// UnwrapLink unwraps Outlook SafeLinks URLs.
func UnwrapLink(val string) string {
	u, err := url.Parse(val)
	if err != nil {
		return val
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasSuffix(host, ".safelinks.protection.outlook.com") {
		q := u.Query()
		if target := q.Get("url"); target != "" {
			return target
		}
	}
	return val
}

// Render produces the complete Markdown document including frontmatter, title, body, and attachment notes.
func Render(msg *eml.Message, category string) ([]byte, Metadata, error) {
	subject := msg.Subject
	var dateStr *string
	if msg.Date != "" {
		if t, err := mail.ParseDate(msg.Date); err == nil {
			formatted := t.Format(time.RFC3339)
			dateStr = &formatted
		} else {
			d := msg.Date
			dateStr = &d
		}
	}

	metadata := Metadata{
		Title:         subject,
		Date:          dateStr,
		From:          msg.From,
		SenderAddress: msg.SenderAddress,
		Category:      category,
		MessageID:     msg.MessageID,
		SourceSHA256:  msg.SHA256,
		LocalSource:   fmt.Sprintf(".local-imports/originals/%s.eml", msg.SHA256),
		Attachments:   msg.Attachments,
	}
	if metadata.Attachments == nil {
		metadata.Attachments = []string{}
	}

	// Format YAML frontmatter using JSON-encoded values (valid YAML, safe from injection/newlines)
	var fmBuf bytes.Buffer
	fmBuf.WriteString("---\n")
	writeYAMLField(&fmBuf, "title", metadata.Title)
	if metadata.Date != nil {
		writeYAMLField(&fmBuf, "date", *metadata.Date)
	} else {
		fmBuf.WriteString("date: null\n")
	}
	writeYAMLField(&fmBuf, "from", metadata.From)
	writeYAMLField(&fmBuf, "sender_address", metadata.SenderAddress)
	writeYAMLField(&fmBuf, "category", metadata.Category)
	writeYAMLField(&fmBuf, "message_id", metadata.MessageID)
	writeYAMLField(&fmBuf, "source_sha256", metadata.SourceSHA256)
	writeYAMLField(&fmBuf, "local_source", metadata.LocalSource)
	attBytes, _ := json.Marshal(metadata.Attachments)
	fmBuf.WriteString(fmt.Sprintf("attachments: %s\n", string(attBytes)))
	fmBuf.WriteString("---\n\n")

	cleanTitle := strings.Join(strings.Fields(subject), " ")
	fmBuf.WriteString(fmt.Sprintf("# %s\n\n", cleanTitle))

	rawBody, isHTML := msg.PreferredBody()
	var bodyMD string
	if isHTML {
		bodyMD = HTMLToMarkdown(rawBody)
	} else {
		bodyMD = strings.TrimSpace(rawBody)
	}

	fmBuf.WriteString(bodyMD)

	if len(metadata.Attachments) > 0 {
		fmBuf.WriteString("\n\n## Attachments\n\nAttachment content remains in the local EML and is not included in this Markdown.\n\n")
		for _, att := range metadata.Attachments {
			attJSON, _ := json.Marshal(att)
			fmBuf.WriteString(fmt.Sprintf("- %s\n", string(attJSON)))
		}
	}

	if strings.TrimSpace(bodyMD) == "" {
		fmBuf.WriteString("\n\nNo readable text body found; inspect the preserved EML.\n")
	} else {
		fmBuf.WriteString("\n")
	}

	return fmBuf.Bytes(), metadata, nil
}

func writeYAMLField(buf *bytes.Buffer, key string, val string) {
	encoded, _ := json.Marshal(val)
	buf.WriteString(fmt.Sprintf("%s: %s\n", key, string(encoded)))
}

// HTMLToMarkdown converts an HTML string into clean Markdown.
func HTMLToMarkdown(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}

	// 1. Clean DOM: remove script, style, head, meta, link, comments
	cleanNode(doc)

	// 2. Flatten Canvas layout wrapper tables
	flattenLayoutTables(doc)

	// 3. Render DOM to Markdown
	var buf bytes.Buffer
	renderNode(doc, &buf, &renderContext{})

	result := buf.String()
	result = reMultipleNewlines.ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

func cleanNode(n *html.Node) {
	var toRemove []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.CommentNode {
			toRemove = append(toRemove, c)
			continue
		}
		if c.Type == html.ElementNode {
			tag := strings.ToLower(c.Data)
			if tag == "script" || tag == "style" || tag == "head" || tag == "meta" || tag == "link" {
				toRemove = append(toRemove, c)
				continue
			}
			// Sanitize links and unwrap SafeLinks
			if tag == "a" {
				sanitizeAnchor(c)
			}
		}
		cleanNode(c)
	}
	for _, child := range toRemove {
		n.RemoveChild(child)
	}
}

func sanitizeAnchor(n *html.Node) {
	for i, attr := range n.Attr {
		if strings.ToLower(attr.Key) == "href" {
			unwrapped := UnwrapLink(attr.Val)
			u, err := url.Parse(unwrapped)
			if err == nil {
				scheme := strings.ToLower(u.Scheme)
				if scheme == "http" || scheme == "https" || scheme == "mailto" || scheme == "" {
					n.Attr[i].Val = unwrapped
					return
				}
			}
			// Invalid scheme: strip href
			n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
			return
		}
	}
}

func flattenLayoutTables(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Table {
			if isCanvasWrapperTable(c) {
				// Convert this table and its tr/td to div
				convertTableToDiv(c)
			}
		}
		flattenLayoutTables(c)
	}
}

func isCanvasWrapperTable(n *html.Node) bool {
	classAttr := getAttr(n, "class")
	styleAttr := strings.ToLower(getAttr(n, "style"))
	roleAttr := strings.ToLower(getAttr(n, "role"))

	if roleAttr == "presentation" {
		return true
	}

	classes := strings.Fields(classAttr)
	hasWrapperClass := false
	for _, cls := range classes {
		switch strings.ToLower(cls) {
		case "body", "main", "logo", "footer":
			hasWrapperClass = true
		}
	}

	hasMSO := strings.Contains(styleAttr, "mso-table-lspace")
	hasDirectTH := tableHasDirectTH(n)

	return hasWrapperClass && hasMSO && !hasDirectTH
}

func tableHasDirectTH(table *html.Node) bool {
	var check func(*html.Node) bool
	check = func(curr *html.Node) bool {
		for c := curr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if c.DataAtom == atom.Table && c != table {
					continue // nested table
				}
				if c.DataAtom == atom.Th {
					return true
				}
				if check(c) {
					return true
				}
			}
		}
		return false
	}
	return check(table)
}

func convertTableToDiv(table *html.Node) {
	table.Data = "div"
	table.DataAtom = atom.Div

	var convertRows func(*html.Node)
	convertRows = func(curr *html.Node) {
		for c := curr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if c.DataAtom == atom.Table {
					continue // leave nested tables alone
				}
				switch c.DataAtom {
				case atom.Thead, atom.Tbody, atom.Tfoot, atom.Tr, atom.Td, atom.Th:
					c.Data = "div"
					c.DataAtom = atom.Div
				}
				convertRows(c)
			}
		}
	}
	convertRows(table)
}

type renderContext struct {
	inLink  bool
	linkURL string
}

func renderNode(n *html.Node, buf *bytes.Buffer, ctx *renderContext) {
	if n.Type == html.TextNode {
		text := n.Data
		// Normalize spaces
		text = reWhitespace.ReplaceAllString(text, " ")
		buf.WriteString(text)
		return
	}

	if n.Type != html.ElementNode {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderNode(c, buf, ctx)
		}
		return
	}

	tag := strings.ToLower(n.Data)
	switch tag {
	case "p", "div":
		buf.WriteString("\n\n")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderNode(c, buf, ctx)
		}
		buf.WriteString("\n\n")

	case "h1":
		buf.WriteString("\n\n# ")
		renderChildren(n, buf, ctx)
		buf.WriteString("\n\n")
	case "h2":
		buf.WriteString("\n\n## ")
		renderChildren(n, buf, ctx)
		buf.WriteString("\n\n")
	case "h3":
		buf.WriteString("\n\n### ")
		renderChildren(n, buf, ctx)
		buf.WriteString("\n\n")
	case "h4":
		buf.WriteString("\n\n#### ")
		renderChildren(n, buf, ctx)
		buf.WriteString("\n\n")
	case "h5", "h6":
		buf.WriteString("\n\n##### ")
		renderChildren(n, buf, ctx)
		buf.WriteString("\n\n")

	case "br":
		buf.WriteString("\n")

	case "hr":
		buf.WriteString("\n\n---\n\n")

	case "strong", "b":
		buf.WriteString("**")
		renderChildren(n, buf, ctx)
		buf.WriteString("**")

	case "em", "i":
		buf.WriteString("*")
		renderChildren(n, buf, ctx)
		buf.WriteString("*")

	case "code":
		buf.WriteString("`")
		renderChildren(n, buf, ctx)
		buf.WriteString("`")

	case "blockquote":
		buf.WriteString("\n\n> ")
		var inner bytes.Buffer
		renderChildren(n, &inner, ctx)
		lines := strings.Split(strings.TrimSpace(inner.String()), "\n")
		for i, line := range lines {
			if i > 0 {
				buf.WriteString("\n> ")
			}
			buf.WriteString(line)
		}
		buf.WriteString("\n\n")

	case "ul", "ol":
		buf.WriteString("\n\n")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && strings.ToLower(c.Data) == "li" {
				buf.WriteString("- ")
				var itemBuf bytes.Buffer
				renderChildren(c, &itemBuf, ctx)
				buf.WriteString(strings.TrimSpace(itemBuf.String()))
				buf.WriteString("\n")
			}
		}
		buf.WriteString("\n")

	case "a":
		href := getAttr(n, "href")
		if href == "" {
			renderChildren(n, buf, ctx)
			return
		}
		buf.WriteString("[")
		var textBuf bytes.Buffer
		renderChildren(n, &textBuf, ctx)
		linkText := strings.TrimSpace(textBuf.String())
		if linkText == "" {
			linkText = href
		}
		buf.WriteString(linkText)
		buf.WriteString("](")
		buf.WriteString(href)
		buf.WriteString(")")

	case "img":
		// Check for 1x1 tracking pixel
		w := getAttr(n, "width")
		h := getAttr(n, "height")
		if w == "1" || h == "1" {
			return // omit tracking pixel
		}
		alt := strings.TrimSpace(getAttr(n, "alt"))
		if alt == "" {
			alt = "Image; see preserved EML"
		}
		buf.WriteString(fmt.Sprintf(" [Image: %s] ", alt))

	case "table":
		renderTable(n, buf, ctx)

	default:
		renderChildren(n, buf, ctx)
	}
}

func renderChildren(n *html.Node, buf *bytes.Buffer, ctx *renderContext) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderNode(c, buf, ctx)
	}
}

func renderTable(table *html.Node, buf *bytes.Buffer, ctx *renderContext) {
	// Extract rows and cells
	var rows [][]string
	var walkRows func(*html.Node)
	walkRows = func(curr *html.Node) {
		for c := curr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if c.DataAtom == atom.Table && c != table {
					continue // nested table handled separately
				}
				if c.DataAtom == atom.Tr {
					var row []string
					for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
						if cell.Type == html.ElementNode && (cell.DataAtom == atom.Td || cell.DataAtom == atom.Th) {
							var cellBuf bytes.Buffer
							renderChildren(cell, &cellBuf, ctx)
							cleanCell := strings.TrimSpace(cellBuf.String())
							cleanCell = strings.ReplaceAll(cleanCell, "\n", " ")
							cleanCell = strings.ReplaceAll(cleanCell, "|", "\\|")
							row = append(row, cleanCell)
						}
					}
					if len(row) > 0 {
						rows = append(rows, row)
					}
					continue
				}
				walkRows(c)
			}
		}
	}
	walkRows(table)

	if len(rows) == 0 {
		return
	}

	// Calculate max columns
	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return
	}

	buf.WriteString("\n\n")
	// Header row
	header := rows[0]
	for len(header) < maxCols {
		header = append(header, "")
	}
	buf.WriteString("| " + strings.Join(header, " | ") + " |\n")

	// Separator
	seps := make([]string, maxCols)
	for i := range seps {
		seps[i] = "---"
	}
	buf.WriteString("| " + strings.Join(seps, " | ") + " |\n")

	// Data rows
	for _, r := range rows[1:] {
		for len(r) < maxCols {
			r = append(r, "")
		}
		buf.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
	buf.WriteString("\n")
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}
