package cmd

import (
	"strings"
	"testing"
)

func TestBoardUpdateFlags(t *testing.T) {
	if boardUpdateCmd.Flag("overwrite") == nil {
		t.Error("board update 缺少 --overwrite flag")
	}
	if boardUpdateCmd.Flag("snapshot") == nil {
		t.Error("board update 缺少 --snapshot flag")
	}
	if boardUpdateCmd.Flag("dry-run") == nil {
		t.Error("board update 缺少 --dry-run flag")
	}
	if boardUpdateCmd.Flag("stdin") == nil {
		t.Error("board update 缺少 --stdin flag")
	}
	if !strings.Contains(boardUpdateCmd.Long, "overwrite: true") {
		t.Errorf("board update Long 说明应包含 overwrite: true 说明: %s", boardUpdateCmd.Long)
	}
}

func TestBoardCreateNotesFlags(t *testing.T) {
	if createBoardNotesCmd.Flag("client-token") == nil {
		t.Error("board create-notes 缺少 --client-token flag")
	}
	if createBoardNotesCmd.Flag("overwrite") == nil {
		t.Error("board create-notes 缺少 --overwrite flag")
	}
	// 验证示例中不包含短 token abc123
	if strings.Contains(createBoardNotesCmd.Long, "--client-token abc123") {
		t.Errorf("board create-notes 示例不应包含短 token abc123")
	}
}

func TestBoardImportDiagramFlags(t *testing.T) {
	if importDiagramCmd.Flag("parse-mode") == nil {
		t.Error("board import 缺少 --parse-mode flag")
	}
	if importDiagramCmd.Flag("overwrite") == nil {
		t.Error("board import 缺少 --overwrite flag")
	}
}
