package cmd

import "testing"

func TestValidateDriveImportSpec(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		docType string
		target  string
		wantErr string
	}{
		{name: "xlsx as docx rejected", file: "./data.xlsx", docType: "docx", wantErr: "文件类型不匹配"},
		{name: "xls bitable rejected", file: "./data.xls", docType: "bitable", wantErr: ".xls 不能导入为 bitable"},
		{name: "base bitable ok", file: "./snapshot.base", docType: "bitable"},
		{name: "pptx slides ok", file: "./deck.pptx", docType: "slides"},
		{name: "md docx ok", file: "./notes.md", docType: "docx"},
		{name: "pptx as docx rejected", file: "./deck.pptx", docType: "docx", wantErr: ".pptx 不能导入为 docx"},
		{name: "unknown ext", file: "./data.rtf", docType: "docx", wantErr: "不支持的文件扩展名"},
		{name: "target-token non-bitable", file: "./data.xlsx", docType: "sheet", target: "bascnxxx", wantErr: "--target-token 仅在 --type bitable"},
		{name: "target-token bitable ok", file: "./data.xlsx", docType: "bitable", target: "bascnxxx"},
		{name: "missing ext", file: "./noext", docType: "docx", wantErr: "必须带扩展名"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDriveImportSpec(tt.file, tt.docType, "", tt.target, "")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !containsStr(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDriveImportFileSize(t *testing.T) {
	tests := []struct {
		name     string
		ext      string
		docType  string
		fileSize int64
		wantErr  bool
	}{
		{name: "docx exceeds 600mb", ext: "docx", docType: "docx", fileSize: driveImport600MB + 1, wantErr: true},
		{name: "docx at 600mb ok", ext: "docx", docType: "docx", fileSize: driveImport600MB},
		{name: "csv sheet exceeds 20mb", ext: "csv", docType: "sheet", fileSize: driveImport20MB + 1, wantErr: true},
		{name: "csv bitable 100mb ok", ext: "csv", docType: "bitable", fileSize: driveImport100MB},
		{name: "csv bitable exceeds 100mb", ext: "csv", docType: "bitable", fileSize: driveImport100MB + 1, wantErr: true},
		{name: "xlsx 800mb ok", ext: "xlsx", docType: "sheet", fileSize: driveImport800MB},
		{name: "pptx exceeds 500mb", ext: "pptx", docType: "slides", fileSize: driveImport500MB + 1, wantErr: true},
		{name: "base exceeds 20mb", ext: "base", docType: "bitable", fileSize: driveImport20MB + 1, wantErr: true},
		{name: "md 20mb ok", ext: "md", docType: "docx", fileSize: driveImport20MB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDriveImportFileSize(tt.ext, tt.docType, tt.fileSize)
			if tt.wantErr && err == nil {
				t.Fatal("expected size error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}
