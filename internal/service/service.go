package service

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"portguard/internal/ops"
	"portguard/internal/proxy"
	"portguard/internal/store"
)

type Service struct {
	St      *store.Store
	Engines map[string]proxy.Engine
	Paths   proxy.Paths
	PanelPort int

	applyMu sync.Mutex // one apply at a time
}

func New(st *store.Store, paths proxy.Paths, panelPort int) *Service {
	return &Service{
		St: st,
		Engines: map[string]proxy.Engine{
			"nginx":   proxy.NginxEngine{},
			"haproxy": proxy.HAProxyEngine{},
		},
		Paths:     paths,
		PanelPort: panelPort,
	}
}

func (s *Service) loadCerts() (map[int64]store.Cert, error) {
	certs := map[int64]store.Cert{}
	list, err := s.St.ListCerts()
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		certs[c.ID] = c
	}
	return certs, nil
}

// EngineValidation is the per-engine outcome of a dry-run: binary validation
// plus a unified diff of the staged config against the live files.
type EngineValidation struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Diff  string `json:"diff,omitempty"`
}

// ValidateOnly renders and validates both engines without touching live files,
// returning the diff ("dry run") for each engine.
func (s *Service) ValidateOnly(actor string) map[string]EngineValidation {
	results := map[string]EngineValidation{}
	for name, eng := range s.Engines {
		results[name] = s.ValidateEngine(name, eng)
	}
	return results
}

// ValidateEngine renders, validates and diffs one engine.
func (s *Service) ValidateEngine(name string, eng proxy.Engine) EngineValidation {
	mappings, err := s.St.ListMappings()
	if err != nil {
		return EngineValidation{OK: false, Error: err.Error()}
	}
	certs, err := s.loadCerts()
	if err != nil {
		return EngineValidation{OK: false, Error: err.Error()}
	}
	staged, err := eng.Render(mappings, certs, s.Paths)
	if err != nil {
		return EngineValidation{OK: false, Error: "render error: " + err.Error()}
	}
	if err := eng.Validate(staged, s.Paths); err != nil {
		return EngineValidation{OK: false, Error: "validation error: " + err.Error()}
	}
	res := EngineValidation{OK: true}
	var diffs []string
	for rel, content := range staged {
		live := eng.LivePath(s.Paths, rel)
		liveText := ""
		if data, err := os.ReadFile(live); err == nil {
			liveText = string(data)
		}
		if d := ops.UnifiedDiff(liveText, content, "live "+live, "new "+live); d != "" {
			diffs = append(diffs, d)
		}
	}
	res.Diff = strings.Join(diffs, "\n")
	return res
}

// ValidateOne validates a single engine (used before a manual reload).
func (s *Service) ValidateOne(engine string) error {
	eng, ok := s.Engines[engine]
	if !ok {
		return fmt.Errorf("unknown engine %q", engine)
	}
	v := s.ValidateEngine(engine, eng)
	if !v.OK {
		return fmt.Errorf("%s", v.Error)
	}
	return nil
}

// engineBinaryMissing reports whether the engine's binary is not installed.
func engineBinaryMissing(name string, p proxy.Paths) bool {
	switch name {
	case "nginx":
		return exec.Command(p.NginxBin, "-v").Run() != nil
	case "haproxy":
		return exec.Command(p.HAProxyBin, "-v").Run() != nil
	}
	return false
}

// BinaryMissing reports whether an engine's binary is not installed.
func BinaryMissing(name string, p proxy.Paths) bool {
	return engineBinaryMissing(name, p)
}

// ApplyAll renders, validates, backs up, atomically applies and reloads both engines.
// On reload failure the previous files are restored and reloaded again.
// An engine whose binary is missing is skipped only when it has no enabled mappings;
// if it has mappings, the apply fails with a clear error instead.
func (s *Service) ApplyAll(actor string) error {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()

	mappings, err := s.St.ListMappings()
	if err != nil {
		return err
	}
	certs, err := s.loadCerts()
	if err != nil {
		return err
	}

	type stagedEngine struct {
		name   string
		engine proxy.Engine
		files  map[string]string
		abs    map[string]string // rel -> absolute live path
	}
	var stagedList []stagedEngine
	var livePaths []string

	// 1) render + validate every engine
	for name, eng := range s.Engines {
		enabledCount := countEngine(mappings, name)
		if engineBinaryMissing(name, s.Paths) {
			if enabledCount > 0 {
				s.St.Audit(actor, "apply", fmt.Sprintf("%s binary not installed but %d mapping(s) use it", name, enabledCount), "error")
				return fmt.Errorf("%s is not installed on this server, but %d enabled mapping(s) use it", name, enabledCount)
			}
			continue // nothing to manage for this engine — skip quietly
		}
		stagedFiles, err := eng.Render(mappings, certs, s.Paths)
		if err != nil {
			s.St.Audit(actor, "apply", "render failed for "+name+": "+err.Error(), "error")
			return fmt.Errorf("render %s: %w", name, err)
		}
		if err := eng.Validate(stagedFiles, s.Paths); err != nil {
			s.St.Audit(actor, "apply", "validation failed for "+name+": "+err.Error(), "error")
			return fmt.Errorf("validate %s: %w", name, err)
		}
		abs := map[string]string{}
		for rel := range stagedFiles {
			live := s.absPath(eng, rel)
			abs[rel] = live
			livePaths = append(livePaths, live)
		}
		stagedList = append(stagedList, stagedEngine{name: name, engine: eng, files: stagedFiles, abs: abs})
	}

	// 2) write cert files used by configs (both engines read from CertsDir)
	if err := s.writeCertFiles(certs); err != nil {
		s.St.Audit(actor, "apply", "cert write failed: "+err.Error(), "error")
		return err
	}

	// 3) backup current live files (recording which ones did NOT exist —
	// those must be deleted, not restored, during rollback)
	backupDir := filepath.Join(s.Paths.BackupsDir, time.Now().Format("20060102-150405"))
	if err := s.backupFiles(backupDir, livePaths); err != nil {
		s.St.Audit(actor, "apply", "backup failed: "+err.Error(), "error")
		return err
	}
	wasNew := map[string]bool{}
	for _, live := range livePaths {
		if _, err := os.Stat(live); os.IsNotExist(err) {
			wasNew[live] = true
		}
	}

	// 4) stage + atomically apply all files
	var applied []string
	restore := func() {
		for _, livePath := range applied {
			if wasNew[livePath] {
				// the file did not exist before this apply: remove it instead
				// of restoring a stale copy (the backup dir has none)
				_ = os.Remove(livePath)
				continue
			}
			bak := filepath.Join(backupDir, livePath)
			if err := copyFile(bak, livePath); err != nil {
				s.St.Audit("system", "apply.rollback", "restore failed for "+livePath+": "+err.Error(), "error")
			}
		}
	}
	for _, se := range stagedList {
		for rel, content := range se.files {
			live := se.abs[rel]
			if err := atomicWrite(live, content); err != nil {
				restore()
				s.St.Audit(actor, "apply", "write failed for "+live+": "+err.Error(), "error")
				return fmt.Errorf("write %s: %w", live, err)
			}
			applied = append(applied, live)
		}
	}

	// 5) reload engines; rollback on failure
	for _, se := range stagedList {
		if err := se.engine.Reload(s.Paths); err != nil {
			restore()
			for _, eng := range s.Engines {
				if rerr := eng.Reload(s.Paths); rerr != nil {
					s.St.Audit("system", "apply.rollback", "reload after rollback failed for "+rerr.Error(), "error")
				}
			}
			s.St.Audit(actor, "apply", "reload failed for "+se.name+", rolled back: "+err.Error(), "error")
			return fmt.Errorf("reload %s (rolled back): %w", se.name, err)
		}
	}

	// 6) prune old apply backups: keep the most recent 20
	pruneBackups(s.Paths.BackupsDir, 20)

	s.St.Audit(actor, "apply", fmt.Sprintf("applied %d mappings (nginx=%d, haproxy=%d)",
		countEnabled(mappings), countEngine(mappings, "nginx"), countEngine(mappings, "haproxy")), "ok")
	return nil
}

func (s *Service) absPath(eng proxy.Engine, rel string) string {
	return eng.LivePath(s.Paths, rel)
}

// pruneBackups removes the oldest timestamp-named backup directories beyond
// keep, so the backups dir doesn't grow unboundedly with every apply.
func pruneBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) == len("20060102-150405") {
			if _, err := time.Parse("20060102-150405", e.Name()); err == nil {
				dirs = append(dirs, e.Name())
			}
		}
	}
	if len(dirs) <= keep {
		return
	}
	sort.Strings(dirs) // timestamp names sort chronologically
	for _, d := range dirs[:len(dirs)-keep] {
		_ = os.RemoveAll(filepath.Join(dir, d))
	}
}

func (s *Service) writeCertFiles(certs map[int64]store.Cert) error {
	if err := os.MkdirAll(s.Paths.CertsDir, 0o750); err != nil {
		return err
	}
	for id, c := range certs {
		if c.CertPEM == "" || c.KeyPEM == "" {
			continue
		}
		crtPath := filepath.Join(s.Paths.CertsDir, fmt.Sprintf("%d.crt", id))
		keyPath := filepath.Join(s.Paths.CertsDir, fmt.Sprintf("%d.key", id))
		pemPath := filepath.Join(s.Paths.CertsDir, fmt.Sprintf("%d.pem", id))
		if err := os.WriteFile(crtPath, []byte(c.CertPEM), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(keyPath, []byte(c.KeyPEM), 0o600); err != nil {
			return err
		}
		// HAProxy needs cert+key concatenated and readable by the haproxy user
		combined := strings.TrimRight(c.CertPEM, "\n") + "\n" + strings.TrimRight(c.KeyPEM, "\n") + "\n"
		if err := os.WriteFile(pemPath, []byte(combined), 0o640); err != nil {
			return err
		}
		// best-effort: make pem group-readable by haproxy
		if g, err := user.LookupGroup("haproxy"); err == nil {
			if gid, err := strconv.Atoi(g.Gid); err == nil {
				_ = os.Chown(pemPath, -1, gid)
			}
		}
	}
	return nil
}

func (s *Service) backupFiles(backupDir string, livePaths []string) error {
	for _, live := range livePaths {
		data, err := os.ReadFile(live)
		if err != nil {
			if os.IsNotExist(err) {
				continue // nothing to back up yet
			}
			return err
		}
		bak := filepath.Join(backupDir, live)
		if err := os.MkdirAll(filepath.Dir(bak), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(bak, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func atomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".portguard-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func countEnabled(ms []store.Mapping) int {
	n := 0
	for _, m := range ms {
		if m.Enabled {
			n++
		}
	}
	return n
}

func countEngine(ms []store.Mapping, engine string) int {
	n := 0
	for _, m := range ms {
		if m.Enabled && m.Engine == engine {
			n++
		}
	}
	return n
}
