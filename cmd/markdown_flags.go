package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/riba2534/feishu-cli/internal/client"
)

var markdownDiffVersionRe = regexp.MustCompile(`^\d{1,19}$`)

func resolveMarkdownFileFlag(contentFile, fileAlias string) (string, error) {
	if contentFile != "" && fileAlias != "" && contentFile != fileAlias {
		return "", fmt.Errorf("--content-file 与 --file 不能同时指定不同值")
	}
	if contentFile != "" {
		return contentFile, nil
	}
	return fileAlias, nil
}

func validateMarkdownFileName(name, flagName string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%s 不能为空", flagName)
	}
	if !strings.HasSuffix(strings.ToLower(trimmed), ".md") {
		return fmt.Errorf("%s 必须以 .md 结尾，得到 %q", flagName, trimmed)
	}
	return nil
}

func validateMarkdownDiffVersionValue(value, flagName string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s 不能为空", flagName)
	}
	if !markdownDiffVersionRe.MatchString(value) {
		return fmt.Errorf("%s 必须是数字版本号", flagName)
	}
	return nil
}

func markdownPreviewDownloadPath(fileToken string) string {
	return "/open-apis/drive/v1/medias/" + fileToken + "/preview_download"
}

func markdownPreviewParams(version string) map[string]any {
	params := map[string]any{"preview_type": "16"}
	if version != "" {
		params["version"] = version
	}
	return params
}

func markdownCreateSpecName(name, contentFile string) (string, error) {
	fileName := strings.TrimSpace(name)
	if fileName == "" && contentFile != "" {
		fileName = filepath.Base(contentFile)
	}
	if fileName == "" {
		return "", fmt.Errorf("--name 必填（使用 --content 时）")
	}
	if err := validateMarkdownFileName(fileName, "--name"); err != nil {
		return "", err
	}
	return fileName, nil
}

func markdownNeedsMultipart(size int64) bool {
	return client.DriveNeedsMultipart(size)
}
