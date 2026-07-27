package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestReplaceExecutableWhileRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows not relevant")
	}
	dir := t.TempDir()
	oldBin := filepath.Join(dir, "cctui")
	newBin := filepath.Join(dir, "payload")

	if err := os.WriteFile(oldBin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newBin, []byte("#!/bin/sh\necho new-version\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(oldBin)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	time.Sleep(100 * time.Millisecond)

	if err := ensureWritable(oldBin); err != nil {
		t.Fatalf("ensureWritable: %v", err)
	}
	if err := replaceExecutable(oldBin, newBin); err != nil {
		t.Fatalf("replaceExecutable: %v", err)
	}

	out, err := exec.Command(oldBin).CombinedOutput()
	if err != nil {
		t.Fatalf("run new: %v %s", err, out)
	}
	if string(out) != "new-version\n" {
		t.Fatalf("got %q", string(out))
	}
}
