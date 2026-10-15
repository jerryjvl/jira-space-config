package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// StateDir is where live runs keep their lock files and audit logs:
// $XDG_STATE_HOME/jira-space-config, ~/.local/state/jira-space-config, or
// %LOCALAPPDATA%\jira-space-config on Windows.
func StateDir(getenv func(string) string) (string, error) {
	if d := getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "jira-space-config"), nil
	}
	if runtime.GOOS == "windows" && getenv("LOCALAPPDATA") != "" {
		return filepath.Join(getenv("LOCALAPPDATA"), "jira-space-config"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding a directory for the audit log: %w (use --log-dir)", err)
	}
	return filepath.Join(home, ".local", "state", "jira-space-config"), nil
}

// lock is the one live run on a site and space, so two terminals cannot
// apply at once.
type lock struct{ path string }

func lockSpace(dir, host, key string) (*lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, host+"-"+key+".lock")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a name made of the site and space
	if errors.Is(err, fs.ErrExist) {
		held, _ := os.ReadFile(path) //nolint:gosec // G304: as above
		return nil, &lockHeld{path: path, by: strings.TrimSpace(string(held))}
	}
	if err != nil {
		return nil, err
	}
	_, err = fmt.Fprintf(f, "process %d, since %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return &lock{path: path}, err
}

func (l *lock) release() { _ = os.Remove(l.path) }

type lockHeld struct{ path, by string }

func (e *lockHeld) Error() string { return "another live run holds " + e.path + " (" + e.by + ")" }

// auditLog is one JSONL file per live run: an intent line before each
// write and a result line after. It never holds secrets.
type auditLog struct {
	path string
	mu   sync.Mutex
	f    *os.File
	base map[string]string
}

func openAudit(dir, site, key string, now time.Time) (*auditLog, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, now.UTC().Format("2006-01-02T150405Z")+"-"+key+".jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // G304: a name made of the time and space
	if err != nil {
		return nil, err
	}
	return &auditLog{path: path, f: f, base: map[string]string{"site": site, "space": key}}, nil
}

// write adds one line; fields are merged over the site and space.
func (a *auditLog) write(fields map[string]any) error {
	line := map[string]any{"time": time.Now().UTC().Format(time.RFC3339Nano)}
	for k, v := range a.base {
		line[k] = v
	}
	for k, v := range fields {
		line[k] = v
	}
	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err = a.f.Write(append(data, '\n'))
	return err
}

func (a *auditLog) close() { _ = a.f.Close() }
