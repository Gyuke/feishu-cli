package client

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestExpectedMultipartBlockNum_OverflowSafe(t *testing.T) {
	// 朴素 (size+blockSize-1)/blockSize 在 MaxInt64 会溢出；除法+余数不能。
	n, err := expectedMultipartBlockNum(math.MaxInt64, 2)
	if err != nil {
		t.Fatalf("MaxInt64/2: %v", err)
	}
	want := int64(math.MaxInt64)/2 + 1
	if n != want {
		t.Fatalf("expected blocks = %d, want %d", n, want)
	}

	n, err = expectedMultipartBlockNum(10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("exact fit = %d, want 1", n)
	}

	n, err = expectedMultipartBlockNum(11, 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("remainder = %d, want 2", n)
	}

	if _, err := expectedMultipartBlockNum(10, 0); err == nil {
		t.Fatal("block_size=0 应失败")
	}
	if _, err := expectedMultipartBlockNum(-1, 10); err == nil {
		t.Fatal("负 size 应失败")
	}
}

func TestParseMultipartSessionFromAPI_InvalidPlanAndOverflow(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		size    int64
		wantErr string
	}{
		{
			name:    "block_size exceeds max int",
			raw:     `{"code":0,"data":{"upload_id":"up","block_size":1e20,"block_num":1}}`,
			size:    10,
			wantErr: "block_size",
		},
		{
			name:    "json.Number overflow",
			raw:     `{"code":0,"data":{"upload_id":"up","block_size":9223372036854775808,"block_num":1}}`,
			size:    10,
			wantErr: "block_size",
		},
		{
			name:    "non-integer block_size",
			raw:     `{"code":0,"data":{"upload_id":"up","block_size":4.5,"block_num":1}}`,
			size:    10,
			wantErr: "block_size",
		},
		{
			name:    "zero block_size",
			raw:     `{"code":0,"data":{"upload_id":"up","block_size":0,"block_num":1}}`,
			size:    10,
			wantErr: "block_size",
		},
		{
			name:    "mismatched block_num",
			raw:     `{"code":0,"data":{"upload_id":"up","block_size":10,"block_num":99}}`,
			size:    10,
			wantErr: "分片计划不一致",
		},
		{
			name:    "missing upload_id",
			raw:     `{"code":0,"data":{"block_size":10,"block_num":1}}`,
			size:    10,
			wantErr: "upload_id",
		},
		{
			name:    "invalid block_num",
			raw:     `{"code":0,"data":{"upload_id":"up","block_size":10,"block_num":0}}`,
			size:    10,
			wantErr: "block_num",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseMultipartSessionFromAPI([]byte(tc.raw), tc.size)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}

	session, err := parseMultipartSessionFromAPI([]byte(`{"code":0,"data":{"upload_id":"up","block_size":10,"block_num":1}}`), 10)
	if err != nil {
		t.Fatalf("valid session: %v", err)
	}
	if session.UploadID != "up" || session.BlockSize != 10 || session.BlockNum != 1 {
		t.Fatalf("session = %+v", session)
	}
}

func TestValidateMultipartSession_RejectsOversizedBlockSize(t *testing.T) {
	maxInt := maxNativeInt64()
	if maxInt == math.MaxInt64 {
		t.Skip("oversized block_size guard is only reachable on platforms where int is narrower than int64")
	}
	_, err := validateMultipartSession("up", maxInt+1, 1, 1)
	if err == nil || !strings.Contains(err.Error(), "block_size") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseMultipartSessionFromAPI_ValidJSONNumber(t *testing.T) {
	raw := fmt.Sprintf(`{"code":0,"data":{"upload_id":"up","block_size":%d,"block_num":2}}`, 8)
	session, err := parseMultipartSessionFromAPI([]byte(raw), 16)
	if err != nil {
		t.Fatal(err)
	}
	if session.BlockSize != 8 || session.BlockNum != 2 {
		t.Fatalf("session = %+v", session)
	}
}
