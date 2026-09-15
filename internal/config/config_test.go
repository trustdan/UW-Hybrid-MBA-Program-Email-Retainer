package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, edit func(*Config)) string {
	t.Helper()
	dir := t.TempDir()
	c := Defaults()
	c.Input = "input"
	c.Archive = "archive"
	c.Shared = "shared"
	c.State = "state"
	c.Originals = "originals"
	if edit != nil {
		edit(&c)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPathsAndMatching(t *testing.T) {
	path := fixture(t, nil)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Load resolves aliases in the config directory (including Windows 8.3
	// temp paths); compare against that directory's canonical spelling.
	base, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if c.Input != filepath.Join(base, "input") {
		t.Fatal(c.Input)
	}
	if c.Match("Re: WEEKLY ANNOUNCEMENT") != "program-announcement" || c.Match("unrelated") != "" {
		t.Fatal("incorrect subject selection")
	}
	if _, err = os.Stat(c.Archive); !os.IsNotExist(err) {
		t.Fatal("loading created output")
	}
}

func TestRejectUnsafeConfiguration(t *testing.T) {
	cases := map[string]func(*Config){
		"same-input-shared": func(c *Config) { c.Shared = c.Input },
		"input-in-shared":   func(c *Config) { c.Input = "shared/input" },
		"archive-in-input":  func(c *Config) { c.Archive = "input/archive" },
		"shared-in-archive": func(c *Config) { c.Shared = "archive/shared" },
		"state-in-shared":   func(c *Config) { c.State = "shared/state" },
		"same":              func(c *Config) { c.State = c.Archive },
		"empty":             func(c *Config) { c.Input = "" },
		"rules":             func(c *Config) { c.Rules = nil },
		"category":          func(c *Config) { c.Rules[0].Category = "../escape" },
		"version":           func(c *Config) { c.Version = 2 },
		"timeout":           func(c *Config) { c.TimeoutSeconds = 0 },
		"limit":             func(c *Config) { c.MaxMessageBytes = -1 },
		"git_no_remote":     func(c *Config) { c.Git.Enabled = true; c.Git.Remote = "" },
		"git_no_branch":     func(c *Config) { c.Git.Enabled = true; c.Git.Branch = "" },
		"git_blank_remote":  func(c *Config) { c.Git.Enabled = true; c.Git.Remote = " \t" },
		"git_blank_branch":  func(c *Config) { c.Git.Enabled = true; c.Git.Branch = " \t" },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(fixture(t, edit)); err == nil {
				t.Fatalf("%s: accepted invalid config", name)
			}
		})
	}
}

func TestGitPublishingEnabled(t *testing.T) {
	c, err := Load(fixture(t, func(c *Config) { c.Git.Enabled = true }))
	if err != nil {
		t.Fatalf("valid Git publishing configuration rejected: %v", err)
	}
	if !c.Git.Enabled || c.Git.Remote != "origin" || c.Git.Branch != "main" {
		t.Fatalf("Git configuration changed: %+v", c.Git)
	}
}

func TestNestedSharedInsideInputAllowedAndPruned(t *testing.T) {
	path := fixture(t, func(c *Config) {
		c.Shared = "input/Markdown"
	})
	c, err := Load(path)
	if err != nil {
		t.Fatalf("expected nested shared inside input to be valid: %v", err)
	}
	if !c.IsSharedNested() {
		t.Fatal("expected IsSharedNested() to be true")
	}
	// Test pruning
	sharedDir := filepath.Join(c.Input, "Markdown")
	if !c.IsPruned(sharedDir) {
		t.Fatalf("expected shared directory %s to be pruned", sharedDir)
	}
	childInShared := filepath.Join(sharedDir, "canvas-digest")
	if !c.IsPruned(childInShared) {
		t.Fatalf("expected child of shared %s to be pruned", childInShared)
	}
	fileInInput := filepath.Join(c.Input, "message.eml")
	if c.IsPruned(fileInInput) {
		t.Fatalf("expected input file %s NOT to be pruned", fileInInput)
	}
	otherDirInInput := filepath.Join(c.Input, "other-folder")
	if c.IsPruned(otherDirInInput) {
		t.Fatalf("expected other folder %s NOT to be pruned", otherDirInInput)
	}
}

func TestRejectUnknownAndTrailingJSON(t *testing.T) {
	for _, extra := range []string{`,"typo":true}`, `} {}`, `} garbage`} {
		t.Run(extra, func(t *testing.T) {
			path := fixture(t, nil)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, []byte(strings.TrimSuffix(string(data), "}")+extra), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = Load(path); err == nil {
				t.Fatal("accepted malformed configuration")
			}
		})
	}
}

func TestExistingFileCannotBeOutputAncestor(t *testing.T) {
	path := fixture(t, nil)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "archive"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("accepted file as directory")
	}
}

func TestLinkOverlap(t *testing.T) {
	path := fixture(t, nil)
	dir := filepath.Dir(path)
	if err := os.Mkdir(filepath.Join(dir, "input"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "input"), filepath.Join(dir, "shared")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("accepted link into input")
	}
}
