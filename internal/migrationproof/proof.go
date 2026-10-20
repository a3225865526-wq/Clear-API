// Package migrationproof produces an opt-in, instance-bound startup migration
// completion record. It never infers completion from application logs or DDL.
package migrationproof

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const Path = "/tmp/clearapi-migration-proof.json"

var phases = [...]string{"main_database", "retired_options", "log_database"}
var challengePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var mu sync.Mutex
var active *attempt

type attempt struct {
	challenge string
	path      string
	completed int
	failed    bool
}

// Begin is called after environment initialization and before opening any DB.
// The private start file prevents reuse of a stale completion record or a
// second attempt in the same filesystem. Normal startup does no proof I/O.
func Begin(master bool, challenge, path string) error {
	if challenge == "" {
		return nil
	}
	if !master || !challengePattern.MatchString(challenge) {
		return errors.New("migration proof requires a master and a valid challenge")
	}
	mu.Lock()
	defer mu.Unlock()
	if active != nil {
		return errors.New("migration proof already started")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("migration proof path is not fresh")
	}
	f, err := os.OpenFile(path+".started", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("migration proof start unavailable")
	}
	err = f.Close()
	if err != nil {
		return errors.New("migration proof start close failed")
	}
	active = &attempt{challenge: challenge, path: path}
	return nil
}

// End detaches the startup-only observer before normal request handling.
func End() {
	mu.Lock()
	defer mu.Unlock()
	active = nil
}

func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return active != nil
}

// Fail records a real error that a best-effort migration would otherwise swallow.
// No SQL, values, credentials or raw error text enter the completion record.
func Fail() {
	mu.Lock()
	defer mu.Unlock()
	if active != nil {
		active.failed = true
	}
}

func Complete(phase string) {
	mu.Lock()
	defer mu.Unlock()
	if active == nil {
		return
	}
	if active.completed >= len(phases) || phase != phases[active.completed] {
		active.failed = true
		return
	}
	active.completed++
}

// Publish is reached only after all synchronous startup migrations returned.
// A write/sync/close failure blocks startup; partial files remain for diagnosis.
func Publish() error {
	mu.Lock()
	defer mu.Unlock()
	if active == nil {
		return nil
	}
	if active.failed || active.completed != len(phases) {
		return errors.New("migration proof incomplete or failed")
	}
	executable, err := os.Executable()
	if err != nil {
		return errors.New("migration proof executable unavailable")
	}
	f, err := os.Open(executable)
	if err != nil {
		return errors.New("migration proof binary unavailable")
	}
	hash := sha256.New()
	_, err = io.Copy(hash, f)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("migration proof binary hash failed")
	}
	host, err := os.Hostname()
	if err != nil {
		return errors.New("migration proof hostname unavailable")
	}
	document, err := common.Marshal(map[string]any{
		"schema": "clearapi.startup-migration-proof/v1", "status": "success",
		"challenge": active.challenge, "hostname": host, "pid": os.Getpid(),
		"binary_sha256": hex.EncodeToString(hash.Sum(nil)), "phases": phases,
		"main_database": string(common.MainDatabaseType()),
		"log_database":  string(common.LogDatabaseType()),
	})
	if err != nil {
		return errors.New("migration proof encoding failed")
	}
	f, err = os.OpenFile(active.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("migration proof publication unavailable")
	}
	_, writeErr := f.Write(document)
	syncErr := f.Sync()
	closeErr = f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("migration proof persistence failed")
	}
	dir, err := os.Open(filepath.Dir(active.path))
	if err != nil {
		return errors.New("migration proof directory unavailable")
	}
	syncErr = dir.Sync()
	closeErr = dir.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("migration proof directory sync failed")
	}
	return nil
}
