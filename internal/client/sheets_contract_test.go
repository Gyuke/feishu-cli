package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSheetsWriteCells_KeepBooleanType 验证 WriteCells 写入布尔值保持 JSON Boolean 类型
func TestSheetsWriteCells_KeepBooleanType(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"updatedRange":"Sheet1!A1:B1","updatedRows":1,"updatedColumns":2,"updatedCells":2}}`)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	inputValues := [][]any{
		{true, false, "文本", 123},
	}

	res, err := WriteCells(context.Background(), "shtcn_test", "Sheet1!A1:D1", inputValues, "u-test-token")
	if err != nil {
		t.Fatalf("WriteCells error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/open-apis/sheets/v2/spreadsheets/shtcn_test/values" {
		t.Errorf("path = %s, want /open-apis/sheets/v2/spreadsheets/shtcn_test/values", gotPath)
	}

	valueRange, ok := gotBody["valueRange"].(map[string]any)
	if !ok {
		t.Fatalf("valueRange missing: %v", gotBody)
	}
	values, ok := valueRange["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("values format error: %v", valueRange)
	}
	row, ok := values[0].([]any)
	if !ok || len(row) != 4 {
		t.Fatalf("row format error: %v", values[0])
	}

	// 关键断言：第一个必须是布尔 true，第二个必须是布尔 false（不能是 "TRUE" / "FALSE" 字符串）
	if b, ok := row[0].(bool); !ok || !b {
		t.Errorf("row[0] = %v (type %T), want true (type bool)", row[0], row[0])
	}
	if b, ok := row[1].(bool); !ok || b {
		t.Errorf("row[1] = %v (type %T), want false (type bool)", row[1], row[1])
	}
	if s, ok := row[2].(string); !ok || s != "文本" {
		t.Errorf("row[2] = %v, want '文本'", row[2])
	}

	if res.Range != "Sheet1!A1:B1" {
		t.Errorf("res.Range = %s, want Sheet1!A1:B1", res.Range)
	}
}

// TestSheetsAppendCells_KeepBooleanType 验证 AppendCells 追加布尔值保持 JSON Boolean 类型
func TestSheetsAppendCells_KeepBooleanType(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"tableRange":"Sheet1!A1:B2","updates":{"updatedRange":"Sheet1!A2:B2"}}}`)
	}))
	defer srv.Close()
	setupTestConfig(t, srv.URL)

	inputValues := [][]any{
		{false, true},
	}

	_, err := AppendCells(context.Background(), "shtcn_test", "Sheet1!A1:B1", inputValues, "OVERWRITE", "u-test-token")
	if err != nil {
		t.Fatalf("AppendCells error: %v", err)
	}

	valueRange, ok := gotBody["valueRange"].(map[string]any)
	if !ok {
		t.Fatalf("valueRange missing: %v", gotBody)
	}
	values, ok := valueRange["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("values format error: %v", valueRange)
	}
	row, ok := values[0].([]any)
	if !ok || len(row) != 2 {
		t.Fatalf("row format error: %v", values[0])
	}

	if b, ok := row[0].(bool); !ok || b {
		t.Errorf("row[0] = %v, want false (bool)", row[0])
	}
	if b, ok := row[1].(bool); !ok || !b {
		t.Errorf("row[1] = %v, want true (bool)", row[1])
	}
}

// TestSheetsProtect_FailClosedAndUnsupported 验证 protect 和 unprotect 返回明确 unsupported 并 fail-closed
func TestSheetsProtect_FailClosedAndUnsupported(t *testing.T) {
	setupTestConfig(t, "http://127.0.0.1:9999")

	_, err := CreateProtectedRange(context.Background(), "shtcn_test", []*ProtectedRange{
		{
			SheetID: "sht1",
			Dimension: &Dimension{
				SheetID:        "sht1",
				MajorDimension: "ROWS",
				StartIndex:     0,
				EndIndex:       5,
			},
		},
	})
	if err == nil {
		t.Fatal("CreateProtectedRange 应返回错误 (fail-closed)")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("错误信息应包含 unsupported: %v", err)
	}

	err = DeleteProtectedRange(context.Background(), "shtcn_test", []string{"p1", "p2"})
	if err == nil {
		t.Fatal("DeleteProtectedRange 应返回错误 (fail-closed)")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("错误信息应包含 unsupported: %v", err)
	}
}
