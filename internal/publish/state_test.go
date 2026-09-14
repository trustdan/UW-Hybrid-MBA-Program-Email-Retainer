package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadJournal_RejectsNull(t *testing.T) {
	for name, loader := range map[string]func(string) (*StateJournal, error){
		"live":      LoadJournal,
		"read-only": LoadJournalReadOnly,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "journal.json")
			if err := os.WriteFile(path, []byte("null\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loader(dir); err == nil || !strings.Contains(err.Error(), "corrupt journal") {
				t.Fatalf("expected corrupt journal error, got %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "null\n" {
				t.Fatalf("loader modified invalid journal: data=%q, err=%v", data, err)
			}
		})
	}
}

func TestLoadJournal_FreshMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	sj, err := LoadJournal(dir)
	if err != nil {
		t.Fatalf("expected nil error on fresh state, got: %v", err)
	}
	if len(sj.Records) != 0 {
		t.Fatalf("expected empty records, got %d", len(sj.Records))
	}
	// Verify directory was created
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected state directory to be created: %v", err)
	}
}

func TestLoadJournal_CorruptJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	_ = os.MkdirAll(dir, 0700)
	jPath := filepath.Join(dir, "journal.json")
	if err := os.WriteFile(jPath, []byte("NOT_VALID_JSON{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadJournal(dir)
	if err == nil {
		t.Fatal("expected error on corrupt journal, got nil")
	}
	if !strings.Contains(err.Error(), "corrupt journal") {
		t.Fatalf("expected 'corrupt journal' error, got: %v", err)
	}
}

func TestLoadJournalReadOnly_AbsentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "non-existent-state")
	sj, err := LoadJournalReadOnly(dir)
	if err != nil {
		t.Fatalf("expected nil error on read-only absent state, got: %v", err)
	}
	if len(sj.Records) != 0 {
		t.Fatalf("expected empty records, got %d", len(sj.Records))
	}
	// Critical check: read-only must NOT create the directory!
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("LoadJournalReadOnly created state directory on disk: %s", dir)
	}
}

func TestLoadJournalReadOnly_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	jPath := filepath.Join(dir, "journal.json")
	if err := os.WriteFile(jPath, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadJournalReadOnly(dir)
	if err == nil {
		t.Fatal("expected error on corrupt journal in read-only mode, got nil")
	}
	if !strings.Contains(err.Error(), "corrupt journal") {
		t.Fatalf("expected 'corrupt journal' error, got: %v", err)
	}
}
