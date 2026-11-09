package logging

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jenga201/dinoga/internal/config"
)

func TestParseSchedule(t *testing.T) {
	cases := []struct {
		in   string
		secs int64
	}{
		{"", 0}, {"0", 0}, {"never", 0}, {"off", 0}, {"NO", 0},
		{"hourly", hour},
		{"daily", day},
		{"weekly", week},
		{"monthly", month},
		{"4", 4 * hour},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := ParseSchedule(c.in).seconds; got != c.secs {
			t.Errorf("ParseSchedule(%q) = %d, want %d", c.in, got, c.secs)
		}
	}
}

func TestScheduleDue(t *testing.T) {
	ref := time.Date(2026, 11, 8, 13, 15, 0, 0, time.UTC)
	cases := []struct {
		name string
		s    Schedule
		last time.Time
		now  time.Time
		want bool
	}{
		{"never", ParseSchedule("never"), ref, ref.Add(48 * time.Hour), false},
		{"daily same day", ParseSchedule("daily"), ref, ref.Add(time.Minute), false},
		{"daily next day", ParseSchedule("daily"), ref, ref.Add(24 * time.Hour), true},
		{"daily across year", ParseSchedule("daily"), time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"monthly same month", ParseSchedule("monthly"), ref, ref.AddDate(0, 0, 20), false},
		{"monthly next month", ParseSchedule("monthly"), ref, ref.AddDate(0, 1, 0), true},
		{"weekly next week", ParseSchedule("weekly"), time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), true},
		{"weekly same week", ParseSchedule("weekly"), time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC), false},
		{"hourly not yet", ParseSchedule("hourly"), ref, ref.Add(30 * time.Minute), false},
		{"hourly elapsed", ParseSchedule("hourly"), ref, ref.Add(2 * time.Hour), true},
	}
	for _, c := range cases {
		if got := c.s.Due(c.last, c.now); got != c.want {
			t.Errorf("%s: Due(last=%v, now=%v) = %v, want %v", c.name, c.last, c.now, got, c.want)
		}
	}
}

func TestRotatedFilename(t *testing.T) {
	ref := time.Date(2026, 11, 8, 13, 15, 0, 0, time.Local)
	if got := ParseSchedule("daily").RotatedFilename("/x/a.log", ref); got != "/x/a.log.2026-11-08" {
		t.Errorf("daily name = %q", got)
	}
	if got := ParseSchedule("hourly").RotatedFilename("/x/a.log", ref); got != "/x/a.log.2026-11-08-13" {
		t.Errorf("hourly name = %q", got)
	}
}

func TestRotatingWriterRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	w, err := Open(path, ParseSchedule("hourly"), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	now := time.Date(2026, 11, 8, 13, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return now }
	w.lastRotate = now

	if _, err := w.Write([]byte("old\n")); err != nil {
		t.Fatal(err)
	}

	// Backdate the rotation clock so the next write triggers a rotation.
	w.lastRotate = time.Date(2026, 11, 8, 11, 0, 0, 0, time.UTC)
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}

	archived := path + ".2026-11-08-11"
	if b, err := os.ReadFile(archived); err != nil || string(b) != "old\n" {
		t.Fatalf("archive content = %q, err=%v", b, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "hello\n" {
		t.Fatalf("live log should hold the post-rotation line, got %q", b)
	}
	if !w.lastRotate.Equal(now) {
		t.Fatalf("lastRotate = %v, want %v", w.lastRotate, now)
	}
}

func TestRotatingWriterNoRotationWhenNotDue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	w, err := Open(path, ParseSchedule("daily"), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	now := time.Date(2026, 11, 8, 13, 0, 0, 0, time.Local)
	w.now = func() time.Time { return now }
	w.lastRotate = now

	if _, err := w.Write([]byte("a\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("b\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "a\nb\n" {
		t.Fatalf("log content = %q", b)
	}
}

func TestRotatingWriterExpires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	now := time.Date(2026, 11, 8, 13, 0, 0, 0, time.Local)

	old := path + ".2026-11-01"   // older than 2 days
	fresh := path + ".2026-11-07" // within 2 days
	for _, name := range []string{old, fresh} {
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	w, err := Open(path, ParseSchedule("daily"), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	w.now = func() time.Time { return now }
	w.lastRotate = now.AddDate(0, 0, -1) // force a rotation on next write
	if _, err := w.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old archive should be expired, err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh archive should remain, err=%v", err)
	}
}

func TestLoggerRouting(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		LogFile:         filepath.Join(dir, "main.log"),
		LogFileMessages: filepath.Join(dir, "msg.log"),
		LogFileNotices:  filepath.Join(dir, "ntc.log"),
		LogMessages:     true,
		LogNotices:      true,
	}
	l, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	l.console = io.Discard

	fixed := time.Date(2026, 11, 8, 13, 0, 0, 0, time.Local)
	l.now = func() time.Time { return fixed }

	l.Write([]byte("op line"))
	l.Message("nick!u@h #chan :hi")
	l.Notice("nick!u@h #chan :yo")
	l.Mainf("Stat: ok")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	ts := fixed.Format(timestampLayout)

	main := readFile(t, cfg.LogFile)
	if !strings.Contains(main, "** "+ts+": op line\n") || !strings.Contains(main, "** "+ts+": Stat: ok\n") {
		t.Fatalf("main log = %q", main)
	}
	if strings.Contains(main, "#chan") {
		t.Fatalf("message/notice leaked into main log: %q", main)
	}

	msg := readFile(t, cfg.LogFileMessages)
	if !strings.Contains(msg, "** "+ts+": nick!u@h #chan :hi\n") {
		t.Fatalf("message log = %q", msg)
	}
	ntc := readFile(t, cfg.LogFileNotices)
	if !strings.Contains(ntc, "** "+ts+": nick!u@h #chan :yo\n") {
		t.Fatalf("notice log = %q", ntc)
	}
}

func TestLoggerMessageFallsBackToMain(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		LogFile:     filepath.Join(dir, "main.log"),
		LogMessages: true,
	}
	l, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	l.Message("hello")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cfg.LogFile); !strings.Contains(got, "hello") {
		t.Fatalf("main log missing message: %q", got)
	}
}

func TestLoggerNoLogFile(t *testing.T) {
	// No logfile configured: the logger must tolerate nil rotating writers
	// (console only) without a typed-nil panic.
	l, err := New(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	l.console = io.Discard

	for i := 0; i < 3; i++ {
		l.Write([]byte("op"))
		l.Message("msg")
		l.Notice("ntc")
		l.Mainf("Stat: ok")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
