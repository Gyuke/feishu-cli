package client

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestFetchMarkdownSource_PreviewTypeAndVersion(t *testing.T) {
	const fileToken = "boxcnMarkdownPreview"
	const userToken = "u-test-token"

	var gotPath, gotPreview, gotVersion, gotAuth string
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tenant_access_token") {
			t.Fatal("显式 User Token 不应请求 tenant token")
		}
		gotPath = r.URL.Path
		gotPreview = r.URL.Query().Get("preview_type")
		gotVersion = r.URL.Query().Get("version")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Disposition", `attachment; filename="notes.md"`)
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "# hello preview")
	})
	defer cleanup()

	data, name, err := FetchMarkdownSource(fileToken, "7633658129540910621", userToken)
	if err != nil {
		t.Fatalf("FetchMarkdownSource: %v", err)
	}
	if string(data) != "# hello preview" {
		t.Fatalf("content = %q", data)
	}
	if name != "notes.md" {
		t.Fatalf("file name = %q, want notes.md", name)
	}
	wantPath := "/open-apis/drive/v1/medias/" + fileToken + "/preview_download"
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	if gotPreview != "16" {
		t.Fatalf("preview_type = %q, want 16", gotPreview)
	}
	if gotVersion != "7633658129540910621" {
		t.Fatalf("version = %q", gotVersion)
	}
	if gotAuth != "Bearer "+userToken {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestFetchMarkdownSource_LatestOmitsVersion(t *testing.T) {
	const fileToken = "boxcnMarkdownLatest"
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("version") {
			t.Fatalf("latest fetch must omit version, got %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("preview_type") != "16" {
			t.Fatalf("preview_type = %q", r.URL.Query().Get("preview_type"))
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "latest")
	})
	defer cleanup()

	data, _, err := FetchMarkdownSource(fileToken, "", "u-test-token")
	if err != nil {
		t.Fatalf("FetchMarkdownSource: %v", err)
	}
	if string(data) != "latest" {
		t.Fatalf("content = %q", data)
	}
}

func TestFetchMarkdownSource_BusinessError(t *testing.T) {
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":1061004,"msg":"forbidden"}`)
	})
	defer cleanup()

	_, _, err := FetchMarkdownSource("boxcnDenied", "", "u-test-token")
	if err == nil {
		t.Fatal("expected business error")
	}
	if !strings.Contains(err.Error(), "1061004") {
		t.Fatalf("error = %v", err)
	}
}

func TestUploadMarkdownContent_AllAndOverwriteVersion(t *testing.T) {
	var gotParentType, gotFileToken string
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/drive/v1/files/upload_all" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotParentType = r.FormValue("parent_type")
		gotFileToken = r.FormValue("file_token")
		if r.FormValue("parent_node") != "wikcnMarkdown" {
			t.Fatalf("parent_node = %q", r.FormValue("parent_node"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"file_token":"boxcnUploaded","version":"9"}}`)
	})
	defer cleanup()

	result, err := UploadMarkdownContent(MarkdownUploadSpec{
		FileToken: "boxcnExisting",
		FileName:  "doc.md",
		WikiToken: "wikcnMarkdown",
	}, []byte("# hi"), "u-test-token")
	if err != nil {
		t.Fatalf("UploadMarkdownContent: %v", err)
	}
	if result.FileToken != "boxcnUploaded" || result.Version != "9" {
		t.Fatalf("result = %+v", result)
	}
	if gotParentType != "wiki" {
		t.Fatalf("parent_type = %q, want wiki", gotParentType)
	}
	if gotFileToken != "boxcnExisting" {
		t.Fatalf("file_token field = %q", gotFileToken)
	}
}

func TestUploadMarkdownFile_MultipartBoundary(t *testing.T) {
	tmp := t.TempDir() + "/big.md"
	payload := []byte("chunk-body")
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var sawPrepare, sawPart, sawFinish bool
	var prepareParentNode any
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/files/upload_prepare"):
			sawPrepare = true
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("prepare json: %v body=%s", err, raw)
			}
			prepareParentNode = body["parent_node"]
			if body["parent_type"] != "explorer" {
				t.Fatalf("prepare parent_type = %v", body["parent_type"])
			}
			if body["file_token"] != "boxcnOverwrite" {
				t.Fatalf("prepare file_token = %v", body["file_token"])
			}
			if int64(body["size"].(float64)) != int64(len(payload)) {
				t.Fatalf("prepare size = %v", body["size"])
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"upload_id":"up_1","block_size":10,"block_num":1}}`)
		case strings.HasSuffix(r.URL.Path, "/files/upload_part"):
			sawPart = true
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok"}`)
		case strings.HasSuffix(r.URL.Path, "/files/upload_finish"):
			sawFinish = true
			_, _ = io.WriteString(w, `{"code":0,"data":{"file_token":"boxcnOverwrite","version":"12"}}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()

	orig := maxSingleUploadSize
	maxSingleUploadSize = len(payload) - 1
	defer func() { maxSingleUploadSize = orig }()

	result, err := UploadMarkdownFile(MarkdownUploadSpec{
		FileToken: "boxcnOverwrite",
		FileName:  "big.md",
	}, tmp, "u-test-token")
	if err != nil {
		t.Fatalf("UploadMarkdownFile multipart: %v", err)
	}
	if !sawPrepare || !sawPart || !sawFinish {
		t.Fatalf("missing steps prepare=%v part=%v finish=%v", sawPrepare, sawPart, sawFinish)
	}
	if prepareParentNode != "" {
		t.Fatalf("prepare parent_node = %#v, want empty string", prepareParentNode)
	}
	if result.Version != "12" {
		t.Fatalf("version = %q", result.Version)
	}
}

func TestDriveNeedsMultipartBoundary(t *testing.T) {
	if DriveNeedsMultipart(20 * 1024 * 1024) {
		t.Fatal("exactly 20MB must use upload_all")
	}
	if !DriveNeedsMultipart(20*1024*1024 + 1) {
		t.Fatal("20MB+1 must use multipart")
	}
}
