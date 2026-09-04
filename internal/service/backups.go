package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"portguard/internal/ops"
	"portguard/internal/proxy"
	"portguard/internal/store"
)

// Backups manages the timestamped config backup folders under BackupsDir
// (list / diff / restore / delete), mirroring haproxy-manager's BackupManager.
// Layout: <backups_dir>/<20060102-150405>/<absolute live path>.
type Backups struct {
	Svc *Service
	St  *store.Store
	Now func() time.Time // injectable for tests
}

var tsPattern = "20060102-150405"

type BackupFile struct {
	Engine   string `json:"engine"`
	LivePath string `json:"live_path"`
	Size     int64  `json:"size"`
}

type Backup struct {
	Timestamp string       `json:"timestamp"`
	Files     []BackupFile `json:"files"`
	CreatedAt time.Time    `json:"created_at"`
}

// relLive extracts the live path (POSIX style) recorded inside a backup folder.
func relLive(root, ts, path string) string {
	return strings.ReplaceAll(strings.TrimPrefix(path, filepath.Join(root, ts)), "\\", "/")
}

// List returns every backup folder, newest first.
func (b *Backups) List() ([]Backup, error) {
	root := b.Svc.Paths.BackupsDir
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []Backup{}, nil
		}
		return nil, err
	}
	var out []Backup
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ts := e.Name()
		if _, err := time.Parse(tsPattern, ts); err != nil {
			continue // only touch folders PortGuard created
		}
		backup := Backup{Timestamp: ts}
		_ = filepath.Walk(filepath.Join(root, ts), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil //nolint: nilerr — skip unreadable entries
			}
			live := relLive(root, ts, path)
			eng, _ := b.classify(live)
			backup.Files = append(backup.Files, BackupFile{
				Engine: eng, LivePath: live,
				Size: info.Size(),
			})
			return nil
		})
		if len(backup.Files) > 0 {
			if t, err := time.Parse(tsPattern, ts); err == nil {
				backup.CreatedAt = t
			}
			out = append(out, backup)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out, nil
}

func (b *Backups) classify(livePath string) (engine, stagedRel string) {
	for _, eng := range b.Svc.Engines {
		if rel, ok := eng.StagedRel(b.Svc.Paths, livePath); ok {
			return eng.Name(), rel
		}
	}
	return "", ""
}

// BackupDiff is one file's diff between a backup and the live config.
type BackupDiff struct {
	Engine   string `json:"engine"`
	LivePath string `json:"live_path"`
	Diff     string `json:"diff"`
	Same     bool   `json:"same"`
}

// Diff compares every backed-up file with its current live version.
func (b *Backups) Diff(ts string) ([]BackupDiff, error) {
	dir, err := b.dirFor(ts)
	if err != nil {
		return nil, err
	}
	var out []BackupDiff
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		live := relLive(b.Svc.Paths.BackupsDir, ts, path)
		engName, _ := b.classify(live)
		bakData, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		liveData, _ := os.ReadFile(live)
		d := ops.UnifiedDiff(string(bakData), string(liveData), "backup/"+ts+live, "live"+live)
		out = append(out, BackupDiff{Engine: engName, LivePath: live, Diff: d, Same: d == ""})
		return nil
	})
	return out, err
}

// Restore validates every backed-up file with its engine binary, backs up the
// current state, atomically restores the files and reloads the engines.
func (b *Backups) Restore(actor, ts string) error {
	dir, err := b.dirFor(ts)
	if err != nil {
		return err
	}

	// collect staged files per engine
	type engineStaged struct {
		engine proxy.Engine
		staged map[string]string
		live   []string
	}
	byEngine := map[string]*engineStaged{}
	var order []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		live := relLive(b.Svc.Paths.BackupsDir, ts, path)
		engName, rel := b.classify(live)
		if engName == "" {
			return nil // unknown file — skip
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read backup %s: %w", path, err)
		}
		es := byEngine[engName]
		if es == nil {
			es = &engineStaged{engine: b.Svc.Engines[engName], staged: map[string]string{}}
			byEngine[engName] = es
			order = append(order, engName)
		}
		es.staged[rel] = string(data)
		es.live = append(es.live, live)
		return nil
	})
	if err != nil {
		return err
	}
	if len(byEngine) == 0 {
		return fmt.Errorf("backup %s contains no known config files", ts)
	}

	// 1) validate each staged engine config against the real binary
	for _, name := range order {
		es := byEngine[name]
		if err := es.engine.Validate(es.staged, b.Svc.Paths); err != nil {
			b.St.Audit(actor, "backup.restore", "restore of "+ts+" rejected: "+err.Error(), "error")
			return fmt.Errorf("backup %s failed validation for %s: %w", ts, name, err)
		}
	}

	// 2) safety backup of the current live files
	nowFn := b.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	safetyDir := filepath.Join(b.Svc.Paths.BackupsDir, nowFn().Format(tsPattern))
	var allLive []string
	for _, name := range order {
		allLive = append(allLive, byEngine[name].live...)
	}
	if err := b.Svc.backupFiles(safetyDir, allLive); err != nil {
		return fmt.Errorf("pre-restore backup failed: %w", err)
	}

	// 3) atomically restore, 4) reload with rollback
	var applied []string
	restore := func() {
		for _, live := range applied {
			_ = copyFile(filepath.Join(safetyDir, live), live)
		}
	}
	for _, name := range order {
		es := byEngine[name]
		for rel, content := range es.staged {
			live := es.engine.LivePath(b.Svc.Paths, rel)
			if err := atomicWrite(live, content); err != nil {
				restore()
				b.St.Audit(actor, "backup.restore", "write failed for "+live+": "+err.Error(), "error")
				return fmt.Errorf("write %s: %w", live, err)
			}
			applied = append(applied, live)
		}
	}
	for _, name := range order {
		if err := byEngine[name].engine.Reload(b.Svc.Paths); err != nil {
			restore()
			for _, eng := range b.Svc.Engines {
				_ = eng.Reload(b.Svc.Paths)
			}
			b.St.Audit(actor, "backup.restore", "reload failed restoring "+ts+", rolled back: "+err.Error(), "error")
			return fmt.Errorf("reload %s (rolled back): %w", name, err)
		}
	}
	b.St.Audit(actor, "backup.restore", fmt.Sprintf("restored backup %s (%s)", ts, strings.Join(order, ", ")), "ok")
	return nil
}

// Delete removes one backup folder.
func (b *Backups) Delete(ts string) error {
	if _, err := b.dirFor(ts); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(b.Svc.Paths.BackupsDir, ts))
}

func (b *Backups) dirFor(ts string) (string, error) {
	if _, err := time.Parse(tsPattern, ts); err != nil {
		return "", fmt.Errorf("invalid backup timestamp %q", ts)
	}
	dir := filepath.Join(b.Svc.Paths.BackupsDir, ts)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("backup %s not found", ts)
	}
	return dir, nil
}

// ManagerFor returns a systemd manager for an engine's unit.
func (s *Service) ManagerFor(engine string) (*ops.ServiceManager, error) {
	switch engine {
	case "nginx":
		return ops.NewServiceManager("nginx"), nil
	case "haproxy":
		return ops.NewServiceManager("haproxy"), nil
	}
	return nil, fmt.Errorf("unknown engine %q", engine)
}
