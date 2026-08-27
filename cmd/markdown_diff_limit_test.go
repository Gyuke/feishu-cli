package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMarkdownDiff_FormatAndJQParsedBeforeDownload(t *testing.T) {
	home, restoreHome := isolateMarkdownDriveHome(t)
	defer restoreHome()
	_ = home

	var hits int32
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tenant_access_token") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"tenant_access_token":"t-bot-token","expire":7200}`)
			return
		}
		atomic.AddInt32(&hits, 1)
		t.Errorf("非法 --format 不应发起下载: %s", r.URL.Path)
		http.Error(w, "no download", http.StatusInternalServerError)
	})
	defer cleanup()

	local := filepath.Join(t.TempDir(), "local.md")
	if err := os.WriteFile(local, []byte("# local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := markdownDiffCmd
	_ = cmd.Flags().Set("file-token", "boxcnDiffFormat")
	_ = cmd.Flags().Set("file", local)
	_ = cmd.Flags().Set("format", "not-a-format")
	setCmdAs(t, cmd, "bot")
	defer resetCmdFlag(cmd, "file-token", "file", "format", "as")
	defer setCmdAs(t, cmd, "auto")

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("非法 --format 应在下载前失败")
	}
	if !strings.Contains(err.Error(), "--format") {
		t.Fatalf("error = %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("下载请求数 = %d, want 0", n)
	}
}

func TestMarkdownDiff_InvalidJQBeforeDownload(t *testing.T) {
	home, restoreHome := isolateMarkdownDriveHome(t)
	defer restoreHome()
	_ = home

	var hits int32
	cleanup := stubCmdFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tenant_access_token") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"tenant_access_token":"t-bot-token","expire":7200}`)
			return
		}
		atomic.AddInt32(&hits, 1)
		http.Error(w, "no download", http.StatusInternalServerError)
	})
	defer cleanup()

	local := filepath.Join(t.TempDir(), "local.md")
	if err := os.WriteFile(local, []byte("# local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := markdownDiffCmd
	_ = cmd.Flags().Set("file-token", "boxcnDiffJQ")
	_ = cmd.Flags().Set("file", local)
	_ = cmd.Flags().Set("jq", "[[[")
	setCmdAs(t, cmd, "bot")
	defer resetCmdFlag(cmd, "file-token", "file", "jq", "as")
	defer setCmdAs(t, cmd, "auto")

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("非法 --jq 应在下载前失败")
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("下载请求数 = %d, want 0", n)
	}
}
