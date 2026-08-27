package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

const (
	// markdownSourceFilePreviewType 是官方 native Markdown 源文件预览码（preview_type=16）。
	markdownSourceFilePreviewType    = "16"
	markdownUploadParentTypeExplorer = "explorer"
	markdownUploadParentTypeWiki     = "wiki"
	markdownEmptyContentError        = "Markdown 内容为空，不支持创建或覆盖空 .md 文件"
)

// MarkdownUploadSpec 描述 native Markdown 创建/覆盖的上传目标。
type MarkdownUploadSpec struct {
	FileToken   string // 覆盖时必填；创建时为空
	FileName    string
	FolderToken string // Drive 文件夹；与 WikiToken 互斥
	WikiToken   string // wiki 节点；parent_type=wiki
}

// MarkdownUploadResult 是 upload_all / upload_finish 的归一化结果。
type MarkdownUploadResult struct {
	FileToken string
	Version   string
}

func (spec MarkdownUploadSpec) target() (parentType, parentNode string) {
	if spec.WikiToken != "" {
		return markdownUploadParentTypeWiki, spec.WikiToken
	}
	return markdownUploadParentTypeExplorer, spec.FolderToken
}

func (spec MarkdownUploadSpec) needsMultipart(size int64) bool {
	return size > int64(maxSingleUploadSize)
}

// FetchMarkdownSource 下载 Drive 原生 Markdown 源文件（或指定历史版本）。
//
// 官方协议：GET /open-apis/drive/v1/medias/{token}/preview_download?preview_type=16[&version=N]
// 不再走 /drive/v1/files/{token}/download。
func FetchMarkdownSource(fileToken, version, userAccessToken string) ([]byte, string, error) {
	if strings.TrimSpace(fileToken) == "" {
		return nil, "", fmt.Errorf("file_token 不能为空")
	}
	if version != "" && strings.TrimSpace(version) == "" {
		return nil, "", fmt.Errorf("version 不能为空")
	}

	cli, err := GetClient()
	if err != nil {
		return nil, "", err
	}

	tokenType, opts := resolveTokenOpts(userAccessToken)
	req := &larkcore.ApiReq{
		HttpMethod:                http.MethodGet,
		ApiPath:                   "/open-apis/drive/v1/medias/:file_token/preview_download",
		PathParams:                larkcore.PathParams{},
		QueryParams:               larkcore.QueryParams{},
		SupportedAccessTokenTypes: []larkcore.AccessTokenType{tokenType},
	}
	req.PathParams.Set("file_token", fileToken)
	req.QueryParams.Set("preview_type", markdownSourceFilePreviewType)
	if version != "" {
		req.QueryParams.Set("version", version)
	}

	resp, err := cli.Do(ContextWithTimeout(downloadTimeout), req, opts...)
	if err != nil {
		return nil, "", fmt.Errorf("下载 Markdown 源文件失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("下载 Markdown 源文件失败: HTTP %d, body: %s", resp.StatusCode, string(resp.RawBody))
	}
	if code, msg, isErr := parseDownloadJSONError(resp.Header, resp.RawBody); isErr {
		return nil, "", fmt.Errorf("下载 Markdown 源文件失败: code=%d, msg=%s", code, msg)
	}

	fallback := fileToken + ".md"
	fileName := fileNameFromDownloadHeader(resp.Header, fallback)
	return resp.RawBody, fileName, nil
}

// FetchFileContent 把一个 Drive 原生 Markdown 文件的最新内容下载到内存。
// 走 preview_download?preview_type=16。
func FetchFileContent(fileToken string, userAccessToken string) ([]byte, error) {
	data, _, err := FetchMarkdownSource(fileToken, "", userAccessToken)
	return data, err
}

// FetchFileVersionContent 把一个 Drive 原生 Markdown 文件指定历史版本下载到内存。
// 走 preview_download?preview_type=16&version=N。
func FetchFileVersionContent(fileToken, version, userAccessToken string) ([]byte, error) {
	data, _, err := FetchMarkdownSource(fileToken, version, userAccessToken)
	return data, err
}

// FetchMarkdownFileName 通过 metas/batch_query 读取现有 .md 文件名（title）。
func FetchMarkdownFileName(fileToken, userAccessToken string) (string, error) {
	title, err := FetchDocMetaTitle(fileToken, "file", userAccessToken)
	if err != nil {
		return "", fmt.Errorf("读取现有 Markdown 文件名失败: %w", err)
	}
	return strings.TrimSpace(title), nil
}

// UploadMarkdownContent 把内存中的 Markdown 字节上传（创建或覆盖）。
// size > 20MB 走 files/upload_prepare → upload_part → upload_finish。
func UploadMarkdownContent(spec MarkdownUploadSpec, content []byte, userAccessToken string) (MarkdownUploadResult, error) {
	if err := validateNonEmptyMarkdownSize(int64(len(content))); err != nil {
		return MarkdownUploadResult{}, err
	}
	if spec.FileName == "" {
		return MarkdownUploadResult{}, fmt.Errorf("file_name 不能为空")
	}
	open := func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(content)), nil
	}
	size := int64(len(content))
	if spec.needsMultipart(size) {
		return uploadMarkdownMultipart(spec, size, open, userAccessToken)
	}
	return uploadMarkdownAll(spec, size, open, userAccessToken)
}

// UploadMarkdownFile 把本地 .md 文件上传（创建或覆盖）。>20MB 自动分片。
func UploadMarkdownFile(spec MarkdownUploadSpec, filePath string, userAccessToken string) (MarkdownUploadResult, error) {
	if spec.FileName == "" {
		spec.FileName = filepath.Base(filePath)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return MarkdownUploadResult{}, fmt.Errorf("读取本地文件失败: %w", err)
	}
	if info.IsDir() {
		return MarkdownUploadResult{}, fmt.Errorf("本地路径必须指向文件，不是目录")
	}
	if err := validateNonEmptyMarkdownSize(info.Size()); err != nil {
		return MarkdownUploadResult{}, err
	}
	open := func() (io.ReadCloser, error) {
		return os.Open(filePath)
	}
	if spec.needsMultipart(info.Size()) {
		return uploadMarkdownMultipart(spec, info.Size(), open, userAccessToken)
	}
	return uploadMarkdownAll(spec, info.Size(), open, userAccessToken)
}

// OverwriteFileWithToken 覆盖现有 Drive 文件（如 .md）的内容。
//
// 官方协议仍是 POST /open-apis/drive/v1/files/upload_all，带 file_token 原地覆盖；
// 超过 20MB 改走 upload_prepare / upload_part / upload_finish，并同样携带 file_token。
func OverwriteFileWithToken(fileToken, fileName string, content []byte, userAccessToken string) (string, error) {
	result, err := UploadMarkdownContent(MarkdownUploadSpec{
		FileToken: fileToken,
		FileName:  fileName,
	}, content, userAccessToken)
	if err != nil {
		return "", err
	}
	return result.FileToken, nil
}

// OverwriteFileFromPathWithToken 把本地文件内容覆盖到远端 Drive 文件。
func OverwriteFileFromPathWithToken(filePath, fileToken, fileName, userAccessToken string) (string, error) {
	result, err := UploadMarkdownFile(MarkdownUploadSpec{
		FileToken: fileToken,
		FileName:  fileName,
	}, filePath, userAccessToken)
	if err != nil {
		return "", err
	}
	return result.FileToken, nil
}

func validateNonEmptyMarkdownSize(size int64) error {
	if size == 0 {
		return fmt.Errorf("%s", markdownEmptyContentError)
	}
	return nil
}

func uploadMarkdownAll(spec MarkdownUploadSpec, fileSize int64, openReader func() (io.ReadCloser, error), userAccessToken string) (MarkdownUploadResult, error) {
	if spec.FileToken == "" && spec.FileName == "" {
		return MarkdownUploadResult{}, fmt.Errorf("file_name 不能为空")
	}
	parentType, parentNode := spec.target()

	fileReader, err := openReader()
	if err != nil {
		return MarkdownUploadResult{}, fmt.Errorf("打开 Markdown 内容失败: %w", err)
	}
	defer fileReader.Close()

	cli, err := GetClient()
	if err != nil {
		return MarkdownUploadResult{}, err
	}

	fd := larkcore.NewFormdata().
		AddField("file_name", spec.FileName).
		AddField("parent_type", parentType).
		AddField("parent_node", parentNode).
		AddField("size", fmt.Sprintf("%d", fileSize))
	if spec.FileToken != "" {
		fd.AddField("file_token", spec.FileToken)
	}
	fd.AddFile("file", fileReader)

	tokenType, opts := resolveTokenOpts(userAccessToken)
	resp, err := cli.Post(ContextWithTimeout(downloadTimeout), "/open-apis/drive/v1/files/upload_all", fd, tokenType, opts...)
	if err != nil {
		return MarkdownUploadResult{}, fmt.Errorf("上传 Markdown 失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return MarkdownUploadResult{}, fmt.Errorf("上传 Markdown 失败: HTTP %d, body: %s", resp.StatusCode, string(resp.RawBody))
	}
	return parseMarkdownUploadAPIResponse(resp.RawBody, spec.FileToken != "")
}

func uploadMarkdownMultipart(spec MarkdownUploadSpec, fileSize int64, openReader func() (io.ReadCloser, error), userAccessToken string) (MarkdownUploadResult, error) {
	parentType, parentNode := spec.target()
	cli, err := GetClient()
	if err != nil {
		return MarkdownUploadResult{}, err
	}
	tokenType, opts := resolveTokenOpts(userAccessToken)

	prepareBody := map[string]any{
		"file_name":   spec.FileName,
		"parent_type": parentType,
		"parent_node": parentNode,
		"size":        fileSize,
	}
	if spec.FileToken != "" {
		prepareBody["file_token"] = spec.FileToken
	}

	prepareResp, err := cli.Post(Context(), "/open-apis/drive/v1/files/upload_prepare", prepareBody, tokenType, opts...)
	if err != nil {
		return MarkdownUploadResult{}, fmt.Errorf("初始化 Markdown 分片上传失败: %w", err)
	}
	if prepareResp.StatusCode != http.StatusOK {
		return MarkdownUploadResult{}, fmt.Errorf("初始化 Markdown 分片上传失败: HTTP %d, body: %s", prepareResp.StatusCode, string(prepareResp.RawBody))
	}

	session, err := parseMarkdownMultipartSession(prepareResp.RawBody)
	if err != nil {
		return MarkdownUploadResult{}, err
	}

	expectedBlocks := int((fileSize + session.BlockSize - 1) / session.BlockSize)
	if session.BlockNum != expectedBlocks {
		return MarkdownUploadResult{}, fmt.Errorf("upload_prepare 返回的分片计划不一致: block_size=%d, block_num=%d, expected=%d, size=%d",
			session.BlockSize, session.BlockNum, expectedBlocks, fileSize)
	}

	fmt.Fprintf(os.Stderr, "Markdown 分片上传: %d 片 × %s\n", session.BlockNum, formatSize(int(session.BlockSize)))

	fileReader, err := openReader()
	if err != nil {
		return MarkdownUploadResult{}, fmt.Errorf("打开 Markdown 内容失败: %w", err)
	}
	defer fileReader.Close()

	buffer := make([]byte, int(session.BlockSize))
	remaining := fileSize
	for seq := 0; seq < session.BlockNum; seq++ {
		chunkSize := session.BlockSize
		if remaining > 0 && chunkSize > remaining {
			chunkSize = remaining
		}
		n, readErr := io.ReadFull(fileReader, buffer[:int(chunkSize)])
		if readErr != nil {
			return MarkdownUploadResult{}, fmt.Errorf("读取 Markdown 分片失败: %w", readErr)
		}

		fd := larkcore.NewFormdata().
			AddField("upload_id", session.UploadID).
			AddField("seq", fmt.Sprintf("%d", seq)).
			AddField("size", fmt.Sprintf("%d", n)).
			AddFile("file", bytes.NewReader(buffer[:n]))
		partResp, err := cli.Post(ContextWithTimeout(downloadTimeout), "/open-apis/drive/v1/files/upload_part", fd, tokenType, opts...)
		if err != nil {
			return MarkdownUploadResult{}, fmt.Errorf("上传 Markdown 分片 %d/%d 失败: %w", seq+1, session.BlockNum, err)
		}
		if partResp.StatusCode != http.StatusOK {
			return MarkdownUploadResult{}, fmt.Errorf("上传 Markdown 分片 %d/%d 失败: HTTP %d, body: %s", seq+1, session.BlockNum, partResp.StatusCode, string(partResp.RawBody))
		}
		if code, msg, isErr := parseJSONBusinessError(partResp.RawBody); isErr {
			return MarkdownUploadResult{}, fmt.Errorf("上传 Markdown 分片 %d/%d 失败: code=%d, msg=%s", seq+1, session.BlockNum, code, msg)
		}
		fmt.Fprintf(os.Stderr, "  分片 %d/%d 上传完成 (%s)\n", seq+1, session.BlockNum, formatSize(n))
		remaining -= int64(n)
	}
	if remaining != 0 {
		return MarkdownUploadResult{}, fmt.Errorf("upload_prepare 分片计划不一致: 结束后仍剩 %d 字节", remaining)
	}

	finishResp, err := cli.Post(Context(), "/open-apis/drive/v1/files/upload_finish", map[string]any{
		"upload_id": session.UploadID,
		"block_num": session.BlockNum,
	}, tokenType, opts...)
	if err != nil {
		return MarkdownUploadResult{}, fmt.Errorf("完成 Markdown 分片上传失败: %w", err)
	}
	if finishResp.StatusCode != http.StatusOK {
		return MarkdownUploadResult{}, fmt.Errorf("完成 Markdown 分片上传失败: HTTP %d, body: %s", finishResp.StatusCode, string(finishResp.RawBody))
	}
	return parseMarkdownUploadAPIResponse(finishResp.RawBody, spec.FileToken != "")
}

type markdownMultipartSession struct {
	UploadID  string
	BlockSize int64
	BlockNum  int
}

func parseMarkdownMultipartSession(raw []byte) (markdownMultipartSession, error) {
	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UploadID  string `json:"upload_id"`
			BlockSize int64  `json:"block_size"`
			BlockNum  int    `json:"block_num"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return markdownMultipartSession{}, fmt.Errorf("解析 upload_prepare 响应失败: %w", err)
	}
	if apiResp.Code != 0 {
		return markdownMultipartSession{}, fmt.Errorf("初始化 Markdown 分片上传失败: code=%d, msg=%s", apiResp.Code, apiResp.Msg)
	}
	if apiResp.Data.UploadID == "" || apiResp.Data.BlockSize <= 0 || apiResp.Data.BlockNum <= 0 {
		return markdownMultipartSession{}, fmt.Errorf("upload_prepare 返回数据异常: upload_id=%q, block_size=%d, block_num=%d",
			apiResp.Data.UploadID, apiResp.Data.BlockSize, apiResp.Data.BlockNum)
	}
	return markdownMultipartSession{
		UploadID:  apiResp.Data.UploadID,
		BlockSize: apiResp.Data.BlockSize,
		BlockNum:  apiResp.Data.BlockNum,
	}, nil
}

func parseMarkdownUploadAPIResponse(raw []byte, requireVersion bool) (MarkdownUploadResult, error) {
	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			FileToken   string `json:"file_token"`
			Version     string `json:"version"`
			DataVersion string `json:"data_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return MarkdownUploadResult{}, fmt.Errorf("解析 Markdown 上传响应失败: %w", err)
	}
	if apiResp.Code != 0 {
		return MarkdownUploadResult{}, fmt.Errorf("上传 Markdown 失败: code=%d, msg=%s", apiResp.Code, apiResp.Msg)
	}
	result := MarkdownUploadResult{FileToken: apiResp.Data.FileToken, Version: apiResp.Data.Version}
	if result.Version == "" {
		result.Version = apiResp.Data.DataVersion
	}
	if result.FileToken == "" {
		return MarkdownUploadResult{}, fmt.Errorf("上传 Markdown 失败: 未返回 file_token")
	}
	if requireVersion && result.Version == "" {
		return MarkdownUploadResult{}, fmt.Errorf("覆盖 Markdown 失败: 未返回 version")
	}
	return result, nil
}

func parseJSONBusinessError(raw []byte) (int, string, bool) {
	var e struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &e); err != nil {
		return 0, "", false
	}
	if e.Code != 0 {
		return e.Code, e.Msg, true
	}
	return 0, "", false
}

func fileNameFromDownloadHeader(header http.Header, fallback string) string {
	name := fallback
	if header != nil {
		if headerName := larkcore.FileNameByHeader(header); strings.TrimSpace(headerName) != "" {
			name = headerName
		}
	}
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	name = path.Base(name)
	if name == "" || name == "." || name == ".." {
		return fallback
	}
	return name
}
