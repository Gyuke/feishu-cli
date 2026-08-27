package client

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestUploadMediaForImport_OmitsParentNodeOnUploadAll(t *testing.T) {
	tmp := t.TempDir() + "/notes.md"
	if err := os.WriteFile(tmp, []byte("# import"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var hasParentNode bool
	var parentType, extra string
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/drive/v1/medias/upload_all" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("multipart: %v", err)
		}
		_, hasParentNode = r.MultipartForm.Value["parent_node"]
		parentType = r.FormValue("parent_type")
		extra = r.FormValue("extra")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"file_token":"boxcnImport"}}`)
	})
	defer cleanup()

	token, err := UploadMediaForImport(tmp, "notes.md", "docx", "md", "u-test-token")
	if err != nil {
		t.Fatalf("UploadMediaForImport: %v", err)
	}
	if token != "boxcnImport" {
		t.Fatalf("file_token = %q", token)
	}
	if hasParentNode {
		t.Fatal("upload_all must omit parent_node")
	}
	if parentType != "ccm_import_open" {
		t.Fatalf("parent_type = %q", parentType)
	}
	if extra != `{"file_extension":"md","obj_type":"docx"}` && extra != `{"obj_type":"docx","file_extension":"md"}` {
		t.Fatalf("extra = %q", extra)
	}
}

func TestUploadMediaForImport_MultipartSendsEmptyParentNode(t *testing.T) {
	tmp := t.TempDir() + "/large.xlsx"
	payload := []byte("0123456789ab")
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var prepareParent any
	var sawPrepare, sawPart, sawFinish bool
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/medias/upload_prepare"):
			sawPrepare = true
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			prepareParent = body["parent_node"]
			if body["parent_type"] != "ccm_import_open" {
				t.Fatalf("prepare parent_type = %v", body["parent_type"])
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"upload_id":"up_media","block_size":12,"block_num":1}}`)
		case strings.HasSuffix(r.URL.Path, "/medias/upload_part"):
			sawPart = true
			_, _ = io.WriteString(w, `{"code":0}`)
		case strings.HasSuffix(r.URL.Path, "/medias/upload_finish"):
			sawFinish = true
			_, _ = io.WriteString(w, `{"code":0,"data":{"file_token":"boxcnMediaLarge"}}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()

	orig := maxSingleUploadSize
	maxSingleUploadSize = len(payload) - 1
	defer func() { maxSingleUploadSize = orig }()

	token, err := UploadMediaForImport(tmp, "large.xlsx", "sheet", "xlsx", "u-test-token")
	if err != nil {
		t.Fatalf("multipart import upload: %v", err)
	}
	if token != "boxcnMediaLarge" {
		t.Fatalf("token = %q", token)
	}
	if !sawPrepare || !sawPart || !sawFinish {
		t.Fatalf("missing steps prepare=%v part=%v finish=%v", sawPrepare, sawPart, sawFinish)
	}
	if prepareParent != "" {
		t.Fatalf("prepare parent_node = %#v, want empty string", prepareParent)
	}
}

func TestCreateImportTaskEx_AlwaysSendsPoint(t *testing.T) {
	var body map[string]any
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/drive/v1/import_tasks" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("json: %v %s", err, raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"ticket":"tck_import"}}`)
	})
	defer cleanup()

	ticket, err := CreateImportTaskEx("boxcnFile", "xlsx", "report", "bitable", "", "bascnTarget", "u-test-token")
	if err != nil {
		t.Fatalf("CreateImportTaskEx: %v", err)
	}
	if ticket != "tck_import" {
		t.Fatalf("ticket = %q", ticket)
	}
	point, _ := body["point"].(map[string]any)
	if point == nil {
		t.Fatalf("missing point: %v", body)
	}
	if point["mount_type"] != float64(1) {
		t.Fatalf("mount_type = %v", point["mount_type"])
	}
	if point["mount_key"] != "" {
		t.Fatalf("empty folder must send empty mount_key, got %#v", point["mount_key"])
	}
	if body["token"] != "bascnTarget" {
		t.Fatalf("bitable target token = %v", body["token"])
	}
}

func TestGetRootFolderToken(t *testing.T) {
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/drive/explorer/v2/root_folder/meta" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"token":"fldcnRoot"}}`)
	})
	defer cleanup()

	token, err := GetRootFolderToken("u-test-token")
	if err != nil {
		t.Fatalf("GetRootFolderToken: %v", err)
	}
	if token != "fldcnRoot" {
		t.Fatalf("token = %q", token)
	}
}

func TestGetRootFolderToken_BusinessError(t *testing.T) {
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":1061004,"msg":"forbidden"}`)
	})
	defer cleanup()
	if _, err := GetRootFolderToken("u-test-token"); err == nil || !strings.Contains(err.Error(), "1061004") {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchDocxMarkdownContent_DocsAI(t *testing.T) {
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/open-apis/docs_ai/v1/documents/doxcnMd/fetch" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"format":"markdown"`) {
			t.Fatalf("body = %s", raw)
		}
		if strings.Contains(string(raw), "extra_param") {
			t.Fatal("must not send extra_param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"document":{"content":"# from docs_ai"}}}`)
	})
	defer cleanup()

	content, err := FetchDocxMarkdownContent("doxcnMd", "u-test-token")
	if err != nil {
		t.Fatalf("FetchDocxMarkdownContent: %v", err)
	}
	if content != "# from docs_ai" {
		t.Fatalf("content = %q", content)
	}
}

func TestCreateExportTaskEx_OnlySchema(t *testing.T) {
	var body map[string]any
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/drive/v1/export_tasks" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"ticket":"tck_export"}}`)
	})
	defer cleanup()

	ticket, err := CreateExportTaskEx("bascn1", "bitable", "base", "", true, "u-test-token")
	if err != nil {
		t.Fatalf("CreateExportTaskEx: %v", err)
	}
	if ticket != "tck_export" {
		t.Fatalf("ticket = %q", ticket)
	}
	if body["only_schema"] != true {
		t.Fatalf("only_schema = %v", body["only_schema"])
	}
	if body["file_extension"] != "base" || body["type"] != "bitable" {
		t.Fatalf("body = %v", body)
	}
}

func TestMoveFileWithToken_UsesResolvedFolder(t *testing.T) {
	var gotType, gotFolder string
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/files/boxcnMove/move") {
			t.Fatalf("path = %q", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		gotType, _ = body["type"].(string)
		gotFolder, _ = body["folder_token"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
	})
	defer cleanup()

	taskID, err := MoveFileWithToken("boxcnMove", "fldcnRoot", "docx", "u-test-token")
	if err != nil {
		t.Fatalf("MoveFileWithToken: %v", err)
	}
	if taskID != "" {
		t.Fatalf("file move should not return task_id, got %q", taskID)
	}
	if gotType != "docx" || gotFolder != "fldcnRoot" {
		t.Fatalf("type=%q folder=%q", gotType, gotFolder)
	}
}

func TestParseDocsAIMarkdownContent_FailClosed(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "missing data", raw: "", wantErr: "document"},
		{name: "null data", raw: "null", wantErr: "document"},
		{name: "empty object", raw: `{}`, wantErr: "document"},
		{name: "missing document", raw: `{"other":1}`, wantErr: "document"},
		{name: "null document", raw: `{"document":null}`, wantErr: "document"},
		{name: "missing content", raw: `{"document":{}}`, wantErr: "document.content"},
		{name: "null content", raw: `{"document":{"content":null}}`, wantErr: "document.content"},
		{name: "non-string content", raw: `{"document":{"content":123}}`, wantErr: "document.content"},
		{name: "empty string content is valid", raw: `{"document":{"content":""}}`, want: ""},
		{name: "ok", raw: `{"document":{"content":"# hi"}}`, want: "# hi"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDocsAIMarkdownContent([]byte(tc.raw))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("content = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFetchDocxMarkdownContent_MissingDocumentFailClosed(t *testing.T) {
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
	})
	defer cleanup()

	_, err := FetchDocxMarkdownContent("doxcnMissing", "u-test-token")
	if err == nil || !strings.Contains(err.Error(), "document") {
		t.Fatalf("expected missing document, got %v", err)
	}
}

func TestFetchDocxMarkdownContent_MissingContentFailClosed(t *testing.T) {
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"document":{}}}`)
	})
	defer cleanup()

	_, err := FetchDocxMarkdownContent("doxcnMissingContent", "u-test-token")
	if err == nil || !strings.Contains(err.Error(), "document.content") {
		t.Fatalf("expected missing document.content, got %v", err)
	}
}

func TestDriveTaskCheckPath_AmpersandInjection(t *testing.T) {
	got := driveTaskCheckPath("abc&evil=1")
	if strings.Contains(got, "task_id=abc&") {
		t.Fatalf("task_id was concatenated unsafely: %s", got)
	}
	if !strings.Contains(got, "task_id=abc%26evil%3D1") && !strings.Contains(got, "abc%26evil") {
		t.Fatalf("task_id should be query-escaped, got %s", got)
	}
}

func TestGetDriveTaskCheck_AmpersandInjection(t *testing.T) {
	var rawQuery, taskID, evil string
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/files/task_check") {
			t.Fatalf("path = %q", r.URL.Path)
		}
		rawQuery = r.URL.RawQuery
		taskID = r.URL.Query().Get("task_id")
		evil = r.URL.Query().Get("evil")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"status":"success"}}`)
	})
	defer cleanup()

	status, err := GetDriveTaskCheck("abc&evil=1", "u-test-token")
	if err != nil {
		t.Fatalf("GetDriveTaskCheck: %v", err)
	}
	if status.Status != "success" {
		t.Fatalf("status = %+v", status)
	}
	if taskID != "abc&evil=1" {
		t.Fatalf("task_id query = %q, raw=%s", taskID, rawQuery)
	}
	if evil != "" {
		t.Fatalf("ampersand injected extra query evil=%q raw=%s", evil, rawQuery)
	}
}

func TestUploadMediaForImport_RejectsOversizedBlockSize(t *testing.T) {
	tmp := t.TempDir() + "/large.xlsx"
	payload := []byte("0123456789ab")
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var sawPart bool
	_, cleanup := stubFeishuServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/medias/upload_prepare"):
			_, _ = io.WriteString(w, `{"code":0,"data":{"upload_id":"up_media_overflow","block_size":1e20,"block_num":1}}`)
		case strings.HasSuffix(r.URL.Path, "/medias/upload_part"):
			sawPart = true
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()

	orig := maxSingleUploadSize
	maxSingleUploadSize = len(payload) - 1
	defer func() { maxSingleUploadSize = orig }()

	_, err := UploadMediaForImport(tmp, "large.xlsx", "sheet", "xlsx", "u-test-token")
	if err == nil || !strings.Contains(err.Error(), "block_size") {
		t.Fatalf("expected oversized block_size, got %v", err)
	}
	if sawPart {
		t.Fatal("oversized block_size must fail before upload_part")
	}
}
