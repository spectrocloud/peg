package matcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogDirDefault(t *testing.T) {
	if LogDir != "logs" {
		t.Fatalf("LogDir default = %q, want %q", LogDir, "logs")
	}
}

func TestCreateGatheredLogFileDefaultDir(t *testing.T) {
	oldWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWorkingDir) })

	f, destination, err := createGatheredLogFile(LogDir, "/var/log/messages")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join("logs", "messages")
	if destination != want {
		t.Fatalf("destination = %q, want %q", destination, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("stat gathered log: %v", err)
	}
}

func TestCreateGatheredLogFileAbsoluteNestedDir(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "run", "gathered-logs")
	f, destination, err := createGatheredLogFile(logDir, "/var/log/kern.log")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(logDir, "kern.log")
	if destination != want {
		t.Fatalf("destination = %q, want %q", destination, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("stat gathered log: %v", err)
	}
}

func TestCreateGatheredLogFilePreservesBaseName(t *testing.T) {
	logDir := t.TempDir()
	f, destination, err := createGatheredLogFile(logDir, "/nested/remote/audit.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Base(destination), "audit.json"; got != want {
		t.Fatalf("destination base = %q, want %q", got, want)
	}
}

func TestCreateGatheredLogFileInvalidDir(t *testing.T) {
	parent := t.TempDir()
	notDirectory := filepath.Join(parent, "not-a-directory")
	if err := os.WriteFile(notDirectory, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	f, destination, err := createGatheredLogFile(filepath.Join(notDirectory, "logs"), "/var/log/messages")
	if err == nil {
		t.Fatal("expected an error")
	}
	if f != nil {
		t.Fatal("expected no file on error")
	}
	if destination != "" {
		t.Fatalf("destination = %q, want empty", destination)
	}
}

func TestCreateGatheredLogFileInvalidDestination(t *testing.T) {
	f, destination, err := createGatheredLogFile(t.TempDir(), ".")
	if err == nil {
		t.Fatal("expected an error")
	}
	if f != nil || destination != "" {
		t.Fatalf("result = %#v, %q; want nil, empty", f, destination)
	}
}
