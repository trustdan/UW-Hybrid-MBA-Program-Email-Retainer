// Package publish handles atomic file writes, conflict detection, run locks, and state journals.
package publish

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StateJournal records conversion provenance and generated hashes to detect user edits.
type StateJournal struct {
	mu      sync.Mutex
	path    string
	Records map[string]JournalRecord `json:"records"` // key: source_sha256
}

type JournalRecord struct {
	SourceSHA256   string    `json:"source_sha256"`
	GeneratedSHA256 string   `json:"generated_sha256"`
	Category       string    `json:"category"`
	Filename       string    `json:"filename"`
	ArchiveRel     string    `json:"archive_rel"`
	SharedRel      string    `json:"shared_rel"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// LoadJournal loads or initializes the state journal from the state directory.
func LoadJournal(stateDir string) (*StateJournal, error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	path := filepath.Join(stateDir, "journal.json")
	sj := &StateJournal{
		path:    path,
		Records: make(map[string]JournalRecord),
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &sj.Records)
	}
	return sj, nil
}

// Save writes the journal atomically to disk.
func (sj *StateJournal) Save() error {
	sj.mu.Lock()
	defer sj.mu.Unlock()
	data, err := json.MarshalIndent(sj.Records, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(sj.path, data)
}

// Record updates a journal entry.
func (sj *StateJournal) Record(rec JournalRecord) {
	sj.mu.Lock()
	defer sj.mu.Unlock()
	rec.UpdatedAt = time.Now().UTC()
	sj.Records[rec.SourceSHA256] = rec
}

// Get retrieves a journal entry by source sha256.
func (sj *StateJournal) Get(sourceSHA256 string) (JournalRecord, bool) {
	sj.mu.Lock()
	defer sj.mu.Unlock()
	rec, ok := sj.Records[sourceSHA256]
	return rec, ok
}
