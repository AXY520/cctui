package update

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"v0.4.1":  "0.4.1",
		"V1.2.3":  "1.2.3",
		" 0.1.0 ": "0.1.0",
		"dev":     "dev",
	}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q)=%q want %q", input, got, want)
		}
	}
}

func TestCompareAndHasUpdate(t *testing.T) {
	if Compare("0.4.0", "0.4.1") >= 0 {
		t.Fatal("expected 0.4.0 < 0.4.1")
	}
	if Compare("0.4.1", "0.4.1") != 0 {
		t.Fatal("expected equal")
	}
	if Compare("0.5.0", "0.4.9") <= 0 {
		t.Fatal("expected 0.5.0 > 0.4.9")
	}
	if !HasUpdate("0.4.0", "0.4.1") {
		t.Fatal("expected update available")
	}
	if HasUpdate("0.4.1", "0.4.1") {
		t.Fatal("expected no update")
	}
	if !HasUpdate("dev", "0.4.1") {
		t.Fatal("dev should update to release")
	}
	if HasUpdate("0.4.2", "0.4.1") {
		t.Fatal("local newer should not update")
	}
}

func TestDownloadFileReportsProgress(t *testing.T) {
	payload := make([]byte, 256*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	var reports [][2]int64
	err := downloadFile(srv.Client(), srv.URL, dest, func(done, total int64) {
		reports = append(reports, [2]int64{done, total})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) == 0 {
		t.Fatal("没有任何进度回调")
	}
	last := reports[len(reports)-1]
	if last[0] != int64(len(payload)) || last[1] != int64(len(payload)) {
		t.Fatalf("最终进度=%v，期望 [%d %d]", last, len(payload), len(payload))
	}
	for i := 1; i < len(reports); i++ {
		if reports[i][0] < reports[i-1][0] {
			t.Fatalf("进度倒退: %v -> %v", reports[i-1], reports[i])
		}
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, payload) {
		t.Fatal("下载内容与源不一致")
	}
}

func TestDownloadFileHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	err := downloadFile(srv.Client(), srv.URL, filepath.Join(t.TempDir(), "x"), nil)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("期望 403 错误，实际: %v", err)
	}
}
