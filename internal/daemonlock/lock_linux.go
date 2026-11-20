package daemonlock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ErrHeld means another process already owns this user's router, regardless of
// --data or --listen. The loser must exit before touching credentials or routes.
var ErrHeld = errors.New("маршрутизатор уже обслуживается другим процессом")

// Path is shared across every data/config/listen selection of this UID.
// Use the login runtime directory, not an environment-selected config directory.
func Path() string { return fmt.Sprintf("/run/user/%d/routebox-daemon.lock", os.Getuid()) }

// Acquire holds a stable inode until Close/process exit. Never unlink the file:
// replacing it would allow a second lock owner while the first is still alive.
func Acquire(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	keep := false
	defer func() {
		if !keep {
			_ = file.Close()
		}
	}()
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		return nil, errors.New("недопустимый файл владения маршрутизатором")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrHeld
		}
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	proc, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(proc)[strings.LastIndex(string(proc), ")")+1:])
	if len(fields) <= 19 {
		return nil, errors.New("недоступно время запуска владельца")
	}
	record := struct {
		PID        int    `json:"pid"`
		Executable string `json:"executable"`
		Started    string `json:"started"`
	}{os.Getpid(), strings.TrimSuffix(exe, " (deleted)"), fields[19]}
	body, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if err = file.Chmod(0o600); err != nil {
		return nil, err
	}
	if err = file.Truncate(0); err != nil {
		return nil, err
	}
	if _, err = file.Write(append(body, '\n')); err != nil {
		return nil, fmt.Errorf("запись владельца: %w", err)
	}
	keep = true
	return file, nil
}
