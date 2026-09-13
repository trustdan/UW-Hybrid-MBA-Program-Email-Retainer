// Package config defines the local workflow contract. Loading never writes files.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Rule struct {
	Subject  string `json:"subject"`
	Category string `json:"category"`
}

type Git struct {
	Enabled     bool   `json:"enabled"`
	ArchiveRepo string `json:"archive_repo"`
	ParentRepo  string `json:"parent_repo,omitempty"`
	Remote      string `json:"remote"`
	Branch      string `json:"branch"`
}

type Config struct {
	Version         int    `json:"version"`
	Input           string `json:"input"`
	Archive         string `json:"archive"`
	Shared          string `json:"shared"`
	State           string `json:"state"`
	Originals       string `json:"originals"`
	Rules           []Rule `json:"rules"`
	MaxMessageBytes int64  `json:"max_message_bytes"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	Git             Git    `json:"git"`
}

func Defaults() Config {
	return Config{Version: 1, MaxMessageBytes: 50 << 20, TimeoutSeconds: 600,
		Rules: []Rule{{"Recent Canvas Notifications", "canvas-digest"}, {"Weekly Announcement", "program-announcement"}},
		Git:   Git{Remote: "origin", Branch: "main"}}
}

// Load resolves relative paths against the configuration file, never the CWD.
func Load(path string) (Config, error) {
	c := Defaults()
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return c, errors.New("config must contain exactly one JSON object")
	}
	if c.Version != 1 {
		return c, errors.New("unsupported config version")
	}
	if c.MaxMessageBytes <= 0 || c.MaxMessageBytes > 1<<30 {
		return c, errors.New("max_message_bytes must be between 1 and 1073741824")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 3600 {
		return c, errors.New("timeout_seconds must be between 1 and 3600")
	}
	if len(c.Rules) == 0 {
		return c, errors.New("at least one subject rule is required")
	}
	seen := map[string]bool{}
	for _, r := range c.Rules {
		key := strings.ToLower(r.Subject)
		if strings.TrimSpace(r.Subject) == "" || seen[key] {
			return c, errors.New("empty or duplicate subject rule")
		}
		seen[key] = true
		if r.Category != "canvas-digest" && r.Category != "program-announcement" {
			return c, errors.New("unsupported category (v1 supports the two archive categories)")
		}
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return c, err
	}
	paths := []struct {
		name  string
		value *string
	}{{"input", &c.Input}, {"archive", &c.Archive}, {"shared", &c.Shared}, {"state", &c.State}, {"originals", &c.Originals}}
	for _, p := range paths {
		if strings.TrimSpace(*p.value) == "" {
			return c, fmt.Errorf("%s path is required", p.name)
		}
		*p.value, err = resolve(base, *p.value)
		if err != nil {
			return c, fmt.Errorf("%s: %w", p.name, err)
		}
	}
	for i, p := range paths {
		for _, q := range paths[i+1:] {
			// Special-case: shared may be a strict subdirectory of input (B07a layout)
			if (p.name == "input" && q.name == "shared") || (p.name == "shared" && q.name == "input") {
				inputVal, sharedVal := *p.value, *q.value
				if p.name == "shared" {
					inputVal, sharedVal = *q.value, *p.value
				}
				if samePath(inputVal, sharedVal) {
					return c, errors.New("input and shared paths cannot be the same directory")
				}
				if containsStrict(sharedVal, inputVal) {
					return c, errors.New("input path cannot be inside shared directory")
				}
				// containsStrict(inputVal, sharedVal) is permitted (shared inside input).
				continue
			}
			if samePath(*p.value, *q.value) || containsStrict(*p.value, *q.value) || containsStrict(*q.value, *p.value) {
				return c, fmt.Errorf("%s and %s paths overlap", p.name, q.name)
			}
		}
	}
	if c.Git.Enabled {
		return c, errors.New("Git publishing is not implemented; keep git.enabled false")
	}
	return c, nil
}

// resolve evaluates existing links/junctions, including ancestors of new paths.
func resolve(base, path string) (string, error) {
	if !filepath.IsAbs(path) {
		if filepath.VolumeName(path) != "" || strings.HasPrefix(path, `\`) {
			return "", errors.New("ambiguous rooted path; use an absolute path")
		}
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)
	ancestor := path
	var missing []string
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			canonical, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				target, err := os.Stat(canonical)
				if err != nil || !target.IsDir() {
					return "", errors.New("path ancestor is not a directory")
				}
			}
			for i := len(missing) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, missing[i])
			}
			return canonical, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", err
		}
		missing = append(missing, filepath.Base(ancestor))
		ancestor = parent
	}
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func containsStrict(parent, child string) bool {
	if runtime.GOOS == "windows" {
		parent = strings.ToLower(parent)
		child = strings.ToLower(child)
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// IsSharedNested returns true if the Shared directory is a strict subdirectory of Input.
func (c Config) IsSharedNested() bool {
	return containsStrict(c.Input, c.Shared)
}

// IsPruned returns true if the candidate path matches or is inside the Shared directory
// when Shared is nested within Input.
func (c Config) IsPruned(candidate string) bool {
	if !c.IsSharedNested() {
		return false
	}
	cand := filepath.Clean(candidate)
	shared := filepath.Clean(c.Shared)
	if runtime.GOOS == "windows" {
		cand = strings.ToLower(cand)
		shared = strings.ToLower(shared)
	}
	if cand == shared {
		return true
	}
	rel, err := filepath.Rel(shared, cand)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (c Config) Match(subject string) string {
	for _, r := range c.Rules {
		if strings.Contains(strings.ToLower(subject), strings.ToLower(r.Subject)) {
			return r.Category
		}
	}
	return ""
}
