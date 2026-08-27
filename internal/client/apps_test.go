package client

import (
	"net/http"
	"strings"
	"testing"
)

func TestParseSparkResponse(t *testing.T) {
	t.Run("success returns data subobject", func(t *testing.T) {
		raw := []byte(`{"code":0,"msg":"ok","data":{"app":{"app_id":"app_xxx"}}}`)
		data, err := parseSparkResponse(http.StatusOK, raw)
		if err != nil {
			t.Fatal(err)
		}
		app, ok := data["app"].(map[string]any)
		if !ok || app["app_id"] != "app_xxx" {
			t.Fatalf("data = %#v", data)
		}
	})

	t.Run("biz code!=0 errors with msg", func(t *testing.T) {
		raw := []byte(`{"code":1061002,"msg":"permission denied","data":{}}`)
		_, err := parseSparkResponse(http.StatusOK, raw)
		if err == nil || !strings.Contains(err.Error(), "1061002") {
			t.Fatalf("want code error, got %v", err)
		}
	})

	t.Run("biz code!=0 surfaces data.error.hint when msg empty", func(t *testing.T) {
		raw := []byte(`{"code":1,"msg":"","data":{"error":{"hint":"missing name"}}}`)
		_, err := parseSparkResponse(http.StatusOK, raw)
		if err == nil || !strings.Contains(err.Error(), "missing name") {
			t.Fatalf("want hint surfaced, got %v", err)
		}
	})

	t.Run("HTTP 4xx errors with body preview", func(t *testing.T) {
		_, err := parseSparkResponse(http.StatusForbidden, []byte(`forbidden`))
		if err == nil || !strings.Contains(err.Error(), "403") {
			t.Fatalf("want http error, got %v", err)
		}
	})

	t.Run("no data subobject returns whole result", func(t *testing.T) {
		raw := []byte(`{"code":0,"msg":"ok","page_token":"t"}`)
		data, err := parseSparkResponse(http.StatusOK, raw)
		if err != nil {
			t.Fatal(err)
		}
		if data["page_token"] != "t" {
			t.Fatalf("data = %#v", data)
		}
	})
}

func TestSparkBasePath(t *testing.T) {
	if SparkBasePath != "/open-apis/spark/v1" {
		t.Fatalf("SparkBasePath = %q", SparkBasePath)
	}
}
