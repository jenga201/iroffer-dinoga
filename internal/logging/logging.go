// Package logging implements the bot's operational logging. It mirrors the
// iroffer-dinoex "Logging / stats" feature (docs/REWRITE-PLAN.md #15): a main
// log file with schedule-based rotation and expiry, optional message/notice
// transcript logs, and periodic statistics, every line prefixed with the
// upstream "** YYYY-MM-DD-HH:MM:SS: <text>" timestamp.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jenga201/dinoga/internal/config"
)

const (
	hour  = int64(time.Hour / time.Second)
	day   = 24 * hour
	week  = 7 * day
	month = 30 * day // upstream "monthly" is a fixed 30 days
)

const timestampLayout = "2006-01-02-15:04:05"

// Schedule is a log rotation schedule. The zero value means "never rotate".
type Schedule struct {
	seconds int64
}

// ParseSchedule interprets an iroffer logrotate value: "daily", "weekly",
// "monthly", "hourly", a positive number of hours, or 0/"never"/"off" for no
// rotation. An unrecognized value also disables rotation.
func ParseSchedule(v string) Schedule {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "never", "off", "no":
		return Schedule{}
	case "hourly":
		return Schedule{seconds: hour}
	case "daily":
		return Schedule{seconds: day}
	case "weekly":
		return Schedule{seconds: week}
	case "monthly":
		return Schedule{seconds: month}
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
		return Schedule{seconds: n * hour}
	}
	return Schedule{}
}

// Never reports whether rotation is disabled.
func (s Schedule) Never() bool { return s.seconds == 0 }

// Due reports whether now has crossed the next rotation boundary after last.
func (s Schedule) Due(last, now time.Time) bool {
	if s.seconds == 0 || now.Before(last) {
		return false
	}
	if now.Year() != last.Year() {
		return true
	}
	switch s.seconds {
	case day:
		return !sameDay(now, last)
	case week:
		yN, wN := now.ISOWeek()
		yL, wL := last.ISOWeek()
		return yN != yL || wN != wL
	case month:
		return now.Month() != last.Month()
	default:
		return now.Sub(last) >= time.Duration(s.seconds)*time.Second
	}
}

// RotatedFilename returns the archive name for path's previous period: hourly
// rotations append a "-HH" suffix, longer periods a bare date, matching
// upstream new_logfilename().
func (s Schedule) RotatedFilename(path string, t time.Time) string {
	if s.seconds < day {
		return fmt.Sprintf("%s.%s", path, t.Format("2006-01-02-15"))
	}
	return fmt.Sprintf("%s.%s", path, t.Format("2006-01-02"))
}

func sameDay(a, b time.Time) bool {
	ya, ma, da := a.Date()
	yb, mb, db := b.Date()
	return ya == yb && ma == mb && da == db
}

// RotatingWriter appends to one file, rotating it to a dated archive once the
// schedule fires and deleting archives older than expireDays (0 = keep all).
type RotatingWriter struct {
	mu          sync.Mutex
	path        string
	schedule    Schedule
	expireAfter time.Duration
	f           *os.File
	lastRotate  time.Time
	now         func() time.Time
}

// Open opens path in append mode, seeding the rotation clock from the file's
// modification time so a fresh process does not rotate an existing log again.
func Open(path string, s Schedule, expireDays int) (*RotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	w := &RotatingWriter{
		path:        path,
		schedule:    s,
		expireAfter: time.Duration(expireDays) * 24 * time.Hour,
		f:           f,
		now:         time.Now,
	}
	if st, err := f.Stat(); err == nil {
		w.lastRotate = st.ModTime()
	} else {
		w.lastRotate = w.now()
	}
	return w, nil
}

// Write rotates first (when due) and then appends p to the file.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if now := w.now(); w.schedule.Due(w.lastRotate, now) {
		w.rotate(now)
	}
	if w.f == nil {
		return 0, fmt.Errorf("logging: log file %q is not open", w.path)
	}
	return w.f.Write(p)
}

func (w *RotatingWriter) rotate(now time.Time) {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	if err := os.Rename(w.path, w.schedule.RotatedFilename(w.path, w.lastRotate)); err == nil {
		w.expire()
	}
	if f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		w.f = f
		w.lastRotate = now
	}
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func (w *RotatingWriter) expire() {
	if w.expireAfter <= 0 {
		return
	}
	dir := filepath.Dir(w.path)
	base := filepath.Base(w.path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := w.now().Add(-w.expireAfter)
	for _, e := range entries {
		if t, ok := rotatedTime(base, e.Name()); ok && t.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// rotatedTime parses a "<base>.<date>[-<hour>]" archive name; ok is false when
// name is not a dated archive of base.
func rotatedTime(base, name string) (time.Time, bool) {
	if !strings.HasPrefix(name, base+".") {
		return time.Time{}, false
	}
	suffix := strings.TrimPrefix(name, base+".")
	for _, layout := range []string{"2006-01-02-15", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, suffix, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Logger writes timestamped lines to the console and, when configured, to the
// main log, optional message/notice transcript logs, and a periodic status
// line. It implements io.Writer so it can back the standard log package (run
// with flags cleared so each call is delivered as a single line).
type Logger struct {
	mu       sync.Mutex
	console  io.Writer
	main     *RotatingWriter
	messages *RotatingWriter
	notices  *RotatingWriter
	now      func() time.Time
}

// New builds the logger for cfg, opening the main log (logfile) and the
// message/notice logs (logfile_messages / logfile_notices) when enabled. The
// message/notice writers fall back to the main log when their file is empty.
func New(cfg config.Config) (*Logger, error) {
	l := &Logger{console: os.Stderr, now: time.Now}
	s := ParseSchedule(cfg.LogRotate)

	if cfg.LogFile != "" {
		w, err := Open(cfg.LogFile, s, cfg.ExpireLogFiles)
		if err != nil {
			return nil, fmt.Errorf("logfile %q: %w", cfg.LogFile, err)
		}
		l.main = w
	}
	if cfg.LogMessages {
		w, err := l.openAux(cfg.LogFileMessages, s, cfg.ExpireLogFiles)
		if err != nil {
			l.Close()
			return nil, err
		}
		l.messages = w
	}
	if cfg.LogNotices {
		w, err := l.openAux(cfg.LogFileNotices, s, cfg.ExpireLogFiles)
		if err != nil {
			l.Close()
			return nil, err
		}
		l.notices = w
	}
	return l, nil
}

func (l *Logger) openAux(name string, s Schedule, expireDays int) (*RotatingWriter, error) {
	if name != "" {
		w, err := Open(name, s, expireDays)
		if err != nil {
			return nil, fmt.Errorf("logfile %q: %w", name, err)
		}
		return w, nil
	}
	return l.main, nil
}

// Write implements io.Writer for the standard log package. The standard logger
// delivers one full line per call when its flags are cleared, so each call is
// emitted as a single timestamped line on the console and in the main log.
func (l *Logger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := []byte(l.line(strings.TrimRight(string(p), "\r\n")))
	_, _ = l.console.Write(line)
	l.writeFile(l.main, line)
	return len(p), nil
}

// Message writes an incoming PRIVMSG transcript line to the message log.
func (l *Logger) Message(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeFile(l.messages, []byte(l.line(text)))
}

// Notice writes an incoming NOTICE transcript line to the notice log.
func (l *Logger) Notice(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeFile(l.notices, []byte(l.line(text)))
}

// Mainf writes a formatted line to the main log file only (not the console),
// used for periodic statistics.
func (l *Logger) Mainf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeFile(l.main, []byte(l.line(fmt.Sprintf(format, args...))))
}

func (l *Logger) line(text string) string {
	return fmt.Sprintf("** %s: %s\n", l.now().Format(timestampLayout), text)
}

// writeFile appends line to a rotating log, tolerating a nil writer (the log is
// simply not configured). The concrete pointer type keeps the nil check honest:
// a nil *RotatingWriter passed as io.Writer would otherwise be a non-nil
// interface and dereference nil.
func (l *Logger) writeFile(w *RotatingWriter, line []byte) {
	if w == nil {
		return
	}
	_, _ = w.Write(line)
}

// Close closes every distinct log file.
func (l *Logger) Close() error {
	seen := map[*RotatingWriter]bool{}
	var first error
	for _, w := range []*RotatingWriter{l.main, l.messages, l.notices} {
		if w == nil || seen[w] {
			continue
		}
		seen[w] = true
		if err := w.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
