package cmd

import "testing"

func TestValidateDriveExportFormatMatrix(t *testing.T) {
	tests := []struct {
		name    string
		docType string
		ext     string
		subID   string
		schema  bool
		wantErr bool
	}{
		{name: "docx markdown ok", docType: "docx", ext: "markdown"},
		{name: "doc markdown rejected", docType: "doc", ext: "markdown", wantErr: true},
		{name: "sheet xlsx ok", docType: "sheet", ext: "xlsx"},
		{name: "sheet csv requires sub-id", docType: "sheet", ext: "csv", wantErr: true},
		{name: "sheet csv with sub-id", docType: "sheet", ext: "csv", subID: "0"},
		{name: "bitable base ok", docType: "bitable", ext: "base"},
		{name: "bitable only-schema ok", docType: "bitable", ext: "base", schema: true},
		{name: "only-schema requires base", docType: "bitable", ext: "xlsx", schema: true, wantErr: true},
		{name: "slides pptx ok", docType: "slides", ext: "pptx"},
		{name: "slides pdf ok", docType: "slides", ext: "pdf"},
		{name: "slides markdown rejected", docType: "slides", ext: "markdown", wantErr: true},
		{name: "docx pptx rejected", docType: "docx", ext: "pptx", wantErr: true},
		{name: "sub-id on pdf rejected", docType: "docx", ext: "pdf", subID: "x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDriveExportFormat(tt.docType, tt.ext, tt.subID, tt.schema)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNormalizeDriveExportInput(t *testing.T) {
	sourceType, token, resolved, err := normalizeDriveExportInput("https://example.feishu.cn/wiki/wikiDryRunExport", "", "")
	if err != nil {
		t.Fatalf("wiki url: %v", err)
	}
	if sourceType != "wiki" || token != "wikiDryRunExport" || resolved != "" {
		t.Fatalf("got type=%s token=%s resolved=%s", sourceType, token, resolved)
	}

	sourceType, token, resolved, err = normalizeDriveExportInput("", "docxMdDryRun", "docx")
	if err != nil {
		t.Fatalf("bare token: %v", err)
	}
	if sourceType != "docx" || token != "docxMdDryRun" || resolved != "docx" {
		t.Fatalf("got type=%s token=%s resolved=%s", sourceType, token, resolved)
	}

	if _, _, _, err := normalizeDriveExportInput("", "tok", ""); err == nil {
		t.Fatal("bare token without doc-type should fail")
	}
	if _, _, _, err := normalizeDriveExportInput("https://example.feishu.cn/wiki/w1", "tok", "docx"); err == nil {
		t.Fatal("url and token mutually exclusive")
	}
}

func TestEnsureExportFileExtension(t *testing.T) {
	if got := ensureExportFileExtension("meeting-notes", "markdown"); got != "meeting-notes.md" {
		t.Fatalf("got %q", got)
	}
	if got := ensureExportFileExtension("crm", "base"); got != "crm.base" {
		t.Fatalf("got %q", got)
	}
	if got := ensureExportFileExtension("report.pptx", "pptx"); got != "report.pptx" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyMarkdownPatch(t *testing.T) {
	got, n, err := applyMarkdownPatch("foo TODO bar TODO", "TODO", "DONE", false)
	if err != nil || n != 2 || got != "foo DONE bar DONE" {
		t.Fatalf("literal: got=%q n=%d err=%v", got, n, err)
	}
	got, n, err = applyMarkdownPatch("v1 and v2", `v[0-9]+`, "vX", true)
	if err != nil || n != 2 || got != "vX and vX" {
		t.Fatalf("regex: got=%q n=%d err=%v", got, n, err)
	}
}
