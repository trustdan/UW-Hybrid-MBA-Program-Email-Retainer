package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trustdan/hmba-mail/internal/config"
	"github.com/trustdan/hmba-mail/internal/eml"
	"github.com/trustdan/hmba-mail/internal/gitpub"
	"github.com/trustdan/hmba-mail/internal/naming"
	"github.com/trustdan/hmba-mail/internal/render"
	"github.com/trustdan/hmba-mail/internal/report"
)

// WriteResult indicates the outcome of attempting to write a destination file.
type WriteResult int

const (
	WriteResultCreated WriteResult = iota
	WriteResultUnchanged
	WriteResultUpdated
	WriteResultConflict
)

// AtomicWrite writes data to a temporary file in target's directory and renames it into place.
func AtomicWrite(target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".writing-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Try direct rename first
	if err := os.Rename(tmpPath, target); err == nil {
		return nil
	}

	// Windows fallback if destination file already exists:
	bakPath := filepath.Join(dir, fmt.Sprintf(".backup-%d.tmp", time.Now().UnixNano()))
	if moveErr := os.Rename(target, bakPath); moveErr == nil {
		if replaceErr := os.Rename(tmpPath, target); replaceErr == nil {
			_ = os.Remove(bakPath)
			return nil
		} else {
			_ = os.Rename(bakPath, target) // rollback
			return replaceErr
		}
	}

	return fmt.Errorf("replace %s: %w", target, err)
}

// WriteMarkdownDestination checks for existing files, compares hashes, and writes safely.
func WriteMarkdownDestination(destPath string, data []byte, sourceSHA256 string, journal *StateJournal) (WriteResult, string, error) {
	existingBytes, err := os.ReadFile(destPath)
	if os.IsNotExist(err) {
		if err := AtomicWrite(destPath, data); err != nil {
			return WriteResultConflict, "", err
		}
		return WriteResultCreated, "", nil
	}
	if err != nil {
		return WriteResultConflict, "", err
	}

	if bytes.Equal(existingBytes, data) {
		return WriteResultUnchanged, "", nil
	}

	// File exists and differs
	sourceMarker := fmt.Sprintf(`source_sha256: "%s"`, sourceSHA256)
	if !strings.Contains(string(existingBytes), sourceMarker) {
		return WriteResultConflict, "unrelated destination file or hash collision", nil
	}

	// Has source marker: check if it matches the last generated hash recorded in state
	rec, hasRec := journal.Get(sourceSHA256)
	existingHash := naming.ComputeSHA256(existingBytes)
	if hasRec && existingHash == rec.GeneratedSHA256 {
		// Matches last generated: user hasn't edited, safe to update
		if err := AtomicWrite(destPath, data); err != nil {
			return WriteResultConflict, "", err
		}
		return WriteResultUpdated, "", nil
	}

	// User made manual edits or state unrecorded
	return WriteResultConflict, "destination contains local edits; refusing to overwrite", nil
}

// waitForInputDirectory polls up to maxWait for input directory to exist and be readable.
// This handles OneDrive hydration / startup delay when launched right after logon.
func waitForInputDirectory(ctx context.Context, inputDir string, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		info, err := os.Stat(inputDir)
		if err == nil && info.IsDir() {
			if _, readErr := os.ReadDir(inputDir); readErr == nil {
				return nil
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("input directory %s unavailable after %v", inputDir, maxWait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// readEMLWithRetry reads an EML file, checking size stability and retrying on concurrent mutation.
func readEMLWithRetry(ctx context.Context, path string, maxBytes int64, maxRetries int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		info1, err := os.Stat(path)
		if err != nil {
			lastErr = fmt.Errorf("stat: %w", err)
			time.Sleep(150 * time.Millisecond)
			continue
		}

		if info1.Size() > maxBytes {
			return nil, fmt.Errorf("file size %d exceeds max %d", info1.Size(), maxBytes)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = fmt.Errorf("read: %w", err)
			time.Sleep(150 * time.Millisecond)
			continue
		}

		// Verify file didn't mutate while reading
		info2, err := os.Stat(path)
		if err != nil || info2.Size() != int64(len(data)) || info1.Size() != info2.Size() {
			lastErr = fmt.Errorf("file mutated during read (size changed from %d to %d)", info1.Size(), len(data))
			time.Sleep(150 * time.Millisecond)
			continue
		}

		return data, nil
	}
	return nil, fmt.Errorf("read failed after %d attempts: %w", maxRetries, lastErr)
}

// Backfill copies existing tracked Markdown from Archive into Shared without EML conversion.
func Backfill(ctx context.Context, cfg config.Config, dryRun bool) (*report.BackfillReport, error) {
	rep := &report.BackfillReport{
		ReportVersion: 1,
		Status:        "backfill_complete",
		DryRun:        dryRun,
	}

	if _, err := os.Stat(cfg.Archive); err != nil {
		return nil, fmt.Errorf("archive directory %s unavailable: %w", cfg.Archive, err)
	}

	err := filepath.WalkDir(cfg.Archive, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}

		rel, err := filepath.Rel(cfg.Archive, path)
		if err != nil {
			return err
		}

		rep.TotalScanned++
		data, err := os.ReadFile(path)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("read %s: %v", path, err))
			return nil
		}

		sha256Hex := naming.ComputeSHA256(data)
		targetPath := filepath.Join(cfg.Shared, rel)

		rec := report.BackfillFileRecord{
			ArchiveRel: filepath.ToSlash(rel),
			SharedRel:  filepath.ToSlash(filepath.Join("Markdown", rel)),
			SHA256:     sha256Hex,
		}

		targetBytes, err := os.ReadFile(targetPath)
		if os.IsNotExist(err) {
			rec.Action = "copied"
			if !dryRun {
				if err := AtomicWrite(targetPath, data); err != nil {
					rec.Action = "conflict"
					rec.ConflictMsg = fmt.Sprintf("write error: %v", err)
					rep.Conflicts++
					rep.Errors = append(rep.Errors, rec.ConflictMsg)
				} else {
					rep.Copied++
				}
			} else {
				rep.Copied++
			}
		} else if err == nil {
			if bytes.Equal(targetBytes, data) {
				rec.Action = "unchanged"
				rep.Unchanged++
			} else {
				rec.Action = "conflict"
				rec.ConflictMsg = "differing destination exists"
				rep.Conflicts++
			}
		} else {
			rec.Action = "conflict"
			rec.ConflictMsg = fmt.Sprintf("stat error: %v", err)
			rep.Conflicts++
			rep.Errors = append(rep.Errors, rec.ConflictMsg)
		}

		rep.Files = append(rep.Files, rec)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk archive: %w", err)
	}

	if rep.Conflicts > 0 {
		rep.Status = "backfill_conflicts_detected"
	}

	// In live mode, add folder README if copied
	if !dryRun && rep.Copied > 0 {
		readmePath := filepath.Join(cfg.Shared, "README.md")
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			readmeText := "# HMBA Converted Emails\n\n" +
				"This folder contains Markdown versions of Canvas digests and program announcements for Class 10.\n\n" +
				"- Historical Markdown files may lack locally available originals.\n" +
				"- Attachment files remain in the source emails and are not stored in Markdown.\n" +
				"- Some links require active UW authentication to view.\n"
			_ = AtomicWrite(readmePath, []byte(readmeText))
		}
	}

	return rep, nil
}

// Run discovers EML files in cfg.Input, parses, and writes dual destinations.
func Run(ctx context.Context, cfg config.Config, dryRun bool) (*report.RunReport, error) {
	rep := &report.RunReport{
		ReportVersion: 1,
		Status:        "run_complete",
		DryRun:        dryRun,
		Limitations: []string{
			"File-size stability is a heuristic, not proof that cloud sync is complete",
			"Local completion is not confirmed cloud upload",
		},
	}
	if dryRun {
		rep.Limitations = append(rep.Limitations,
			"Reading OneDrive placeholders may hydrate them locally",
			"No persistent writes or changes were made (dry-run)",
		)
	}

	var logger *Logger
	if !dryRun {
		var err error
		logger, err = NewLogger(cfg.State, 15)
		if err == nil {
			defer logger.Close()
			logger.Logf("hmba-mail run started (input=%s)", cfg.Input)
		}
	}

	// 1. Wait for input availability (bounded wait up to 10s)
	if err := waitForInputDirectory(ctx, cfg.Input, 10*time.Second); err != nil {
		if logger != nil {
			logger.Logf("input directory unavailable: %v", err)
		}
		return nil, fmt.Errorf("input directory unavailable: %w", err)
	}

	var releaseLock func()
	if !dryRun {
		var err error
		releaseLock, err = AcquireLock(cfg.State)
		if err != nil {
			if logger != nil {
				logger.Logf("acquire lock failed: %v", err)
			}
			return nil, err
		}
		defer releaseLock()
	}

	journal, err := LoadJournal(cfg.State)
	if err != nil {
		if logger != nil {
			logger.Logf("load journal failed: %v", err)
		}
		return nil, err
	}

	// Discover EML files with discovery pruning of Shared directory
	var emlPaths []string
	err = filepath.WalkDir(cfg.Input, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return err
		}
		if d.IsDir() {
			if cfg.IsPruned(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) == ".eml" {
			emlPaths = append(emlPaths, path)
		}
		return nil
	})
	if err != nil {
		if logger != nil {
			logger.Logf("scan input directory error: %v", err)
		}
		return nil, fmt.Errorf("scan input directory: %w", err)
	}

	if len(emlPaths) == 0 {
		if logger != nil {
			logger.Logf("scan complete: no EML files found in input directory")
		}
		// Empty input is NOT a failure; clean return with 0 counts
		return rep, nil
	}

	var writtenArchivePaths []string
	for _, emlPath := range emlPaths {
		select {
		case <-ctx.Done():
			rep.Status = "run_cancelled"
			if logger != nil {
				logger.Logf("run cancelled by context")
			}
			return rep, ctx.Err()
		default:
		}

		rep.Counts.Scanned++

		raw, err := readEMLWithRetry(ctx, emlPath, cfg.MaxMessageBytes, 3)
		if err != nil {
			rep.Counts.Failed++
			errMsg := fmt.Sprintf("read %s: %v", emlPath, err)
			rep.Errors = append(rep.Errors, errMsg)
			if logger != nil {
				logger.Logf("warning: %s", errMsg)
			}
			continue
		}

		msg, err := eml.Parse(raw, cfg.Rules)
		if err != nil {
			rep.Counts.Failed++
			errMsg := fmt.Sprintf("parse %s: %v", emlPath, err)
			rep.Errors = append(rep.Errors, errMsg)
			if logger != nil {
				logger.Logf("warning: %s", errMsg)
			}
			continue
		}

		if msg.Category == "" {
			// Unmatched subject, skip safely
			continue
		}
		rep.Counts.Matched++

		when, _, found := naming.DeriveTimestamp(msg.Received, msg.Date)
		filename := naming.FormatDateFilename(when, found, msg.Subject, msg.SHA256)

		mdData, _, err := render.Render(msg, msg.Category)
		if err != nil {
			rep.Counts.Failed++
			errMsg := fmt.Sprintf("render %s: %v", emlPath, err)
			rep.Errors = append(rep.Errors, errMsg)
			if logger != nil {
				logger.Logf("warning: %s", errMsg)
			}
			continue
		}

		rec := report.MessageRecord{
			SourceSHA256: msg.SHA256,
			Subject:      msg.Subject,
			Sender:       msg.From,
			Category:     msg.Category,
			Date:         msg.Date,
			Filename:     filename,
			Attachments:  msg.Attachments,
		}

		if dryRun {
			rep.Counts.Written++
			rec.ArchiveWritten = true
			rec.SharedWritten = true
			rep.Messages = append(rep.Messages, rec)
			continue
		}

		// 1. Save raw original
		origPath := filepath.Join(cfg.Originals, fmt.Sprintf("%s.eml", msg.SHA256))
		if err := AtomicWrite(origPath, raw); err != nil {
			rep.Counts.Failed++
			rec.Error = fmt.Sprintf("write original: %v", err)
			rep.Messages = append(rep.Messages, rec)
			if logger != nil {
				logger.Logf("error: %s", rec.Error)
			}
			continue
		}

		// 2. Write Archive copy
		archPath := filepath.Join(cfg.Archive, msg.Category, filename)
		resArch, conflictArch, err := WriteMarkdownDestination(archPath, mdData, msg.SHA256, journal)
		if err != nil || resArch == WriteResultConflict {
			rep.Counts.Conflicts++
			rec.Error = fmt.Sprintf("archive conflict: %s %v", conflictArch, err)
			rep.Messages = append(rep.Messages, rec)
			if logger != nil {
				logger.Logf("conflict: %s", rec.Error)
			}
			continue
		}

		// 3. Write Shared copy
		sharedPath := filepath.Join(cfg.Shared, msg.Category, filename)
		resShared, conflictShared, err := WriteMarkdownDestination(sharedPath, mdData, msg.SHA256, journal)
		if err != nil || resShared == WriteResultConflict {
			rep.Counts.Conflicts++
			rec.Error = fmt.Sprintf("shared conflict: %s %v", conflictShared, err)
			rep.Messages = append(rep.Messages, rec)
			if logger != nil {
				logger.Logf("conflict: %s", rec.Error)
			}
			continue
		}

		if resArch == WriteResultUnchanged && resShared == WriteResultUnchanged {
			rep.Counts.Unchanged++
		} else {
			rep.Counts.Written++
			if resArch == WriteResultCreated || resArch == WriteResultUpdated {
				writtenArchivePaths = append(writtenArchivePaths, archPath)
			}
		}

		rec.ArchiveWritten = true
		rec.SharedWritten = true
		rep.Messages = append(rep.Messages, rec)

		if logger != nil {
			logger.Logf("published: sha=%s category=%s filename=%s", msg.SHA256[:12], msg.Category, filename)
		}

		// Record in journal
		journal.Record(JournalRecord{
			SourceSHA256:    msg.SHA256,
			GeneratedSHA256: naming.ComputeSHA256(mdData),
			Category:        msg.Category,
			Filename:        filename,
			ArchiveRel:      filepath.ToSlash(filepath.Join(msg.Category, filename)),
			SharedRel:       filepath.ToSlash(filepath.Join(msg.Category, filename)),
		})
	}

	// Automated Git Publishing (Milestone 3)
	if !dryRun && cfg.Git.Enabled {
		repoRoot := cfg.Git.ParentRepo
		if repoRoot == "" {
			var err error
			repoRoot, err = gitpub.FindRepoRoot(ctx, cfg.Archive)
			if err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("git repo root discovery: %v", err))
				rep.Git = &report.GitPublishReport{Enabled: true, Error: err.Error()}
			}
		}

		if repoRoot != "" {
			gitJournal, err := gitpub.LoadGitJournal(cfg.State)
			if err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("git journal load: %v", err))
			} else {
				// First recover any previously interrupted push
				if _, recErr := gitpub.RecoverPending(ctx, repoRoot, cfg.Git.Remote, cfg.Git.Branch, gitJournal); recErr != nil {
					rep.Errors = append(rep.Errors, fmt.Sprintf("git recover pending push: %v", recErr))
				}

				if len(writtenArchivePaths) > 0 {
					if pfErr := gitpub.Preflight(ctx, repoRoot, cfg.Git.Remote, cfg.Git.Branch, writtenArchivePaths, gitJournal); pfErr != nil {
						rep.Errors = append(rep.Errors, fmt.Sprintf("git preflight blocked: %v", pfErr))
						rep.Git = &report.GitPublishReport{Enabled: true, Error: pfErr.Error()}
						rep.Status = "run_completed_git_blocked"
						if logger != nil {
							logger.Logf("git preflight blocked: %v", pfErr)
						}
					} else {
						commitSHA, stErr := gitpub.StageAndCommit(ctx, repoRoot, writtenArchivePaths, gitJournal)
						if stErr != nil {
							rep.Errors = append(rep.Errors, fmt.Sprintf("git commit failed: %v", stErr))
							rep.Git = &report.GitPublishReport{Enabled: true, Error: stErr.Error()}
							if logger != nil {
								logger.Logf("git commit failed: %v", stErr)
							}
						} else if commitSHA != "" {
							if pushErr := gitpub.Push(ctx, repoRoot, cfg.Git.Remote, cfg.Git.Branch, commitSHA, gitJournal); pushErr != nil {
								rep.Errors = append(rep.Errors, fmt.Sprintf("git push failed: %v", pushErr))
								rep.Git = &report.GitPublishReport{Enabled: true, Committed: commitSHA, Pending: commitSHA, Error: pushErr.Error()}
								rep.Status = "run_completed_push_pending"
								if logger != nil {
									logger.Logf("git push failed: %v", pushErr)
								}
							} else {
								rep.Git = &report.GitPublishReport{Enabled: true, Committed: commitSHA, Pushed: true}
								if logger != nil {
									logger.Logf("git published successfully: commit=%s", commitSHA[:8])
								}
							}
						}
					}
				}
			}
		}
	}

	if !dryRun {
		_ = journal.Save()
		// Write last-run.json in State
		lastRunBytes, _ := json.MarshalIndent(rep, "", "  ")
		_ = AtomicWrite(filepath.Join(cfg.State, "last-run.json"), lastRunBytes)
		if logger != nil {
			logger.Logf("run complete: scanned=%d matched=%d written=%d unchanged=%d conflicts=%d failed=%d",
				rep.Counts.Scanned, rep.Counts.Matched, rep.Counts.Written, rep.Counts.Unchanged, rep.Counts.Conflicts, rep.Counts.Failed)
		}
	}

	if rep.Counts.Failed > 0 || rep.Counts.Conflicts > 0 {
		if rep.Status == "run_complete" {
			rep.Status = "run_completed_with_issues"
		}
	}

	return rep, nil
}
