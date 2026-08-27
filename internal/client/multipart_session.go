package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

func maxNativeInt64() int64 {
	return int64(^uint(0) >> 1)
}

type driveMultipartSession struct {
	UploadID  string
	BlockSize int64
	BlockNum  int64
}

func expectedMultipartBlockNum(payloadSize, blockSize int64) (int64, error) {
	maxInt := maxNativeInt64()
	if blockSize <= 0 {
		return 0, fmt.Errorf("upload_prepare 返回的 block_size 无效: %d", blockSize)
	}
	if blockSize > maxInt {
		return 0, fmt.Errorf("upload_prepare 返回的 block_size 超出可分配上限")
	}
	if payloadSize < 0 {
		return 0, fmt.Errorf("payload size 无效: %d", payloadSize)
	}
	if payloadSize == 0 {
		return 0, nil
	}
	n := payloadSize / blockSize
	if payloadSize%blockSize != 0 {
		if n == maxInt {
			return 0, fmt.Errorf("upload_prepare 分片数量溢出")
		}
		n++
	}
	if n > maxInt {
		return 0, fmt.Errorf("upload_prepare 分片数量溢出")
	}
	return n, nil
}

func jsonNumberAsPositiveInt64(v any, maxInt int64) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n <= 0 || n > float64(maxInt) || n != math.Trunc(n) {
			return 0, false
		}
		return int64(n), true
	case int:
		if n <= 0 || int64(n) > maxInt {
			return 0, false
		}
		return int64(n), true
	case int64:
		if n <= 0 || n > maxInt {
			return 0, false
		}
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i <= 0 || i > maxInt {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func validateMultipartSession(uploadID string, blockSize, blockNum, payloadSize int64) (driveMultipartSession, error) {
	if uploadID == "" {
		return driveMultipartSession{}, fmt.Errorf("upload_prepare 返回数据异常: upload_id 为空")
	}
	maxInt := maxNativeInt64()
	if blockSize <= 0 {
		return driveMultipartSession{}, fmt.Errorf("upload_prepare 返回的 block_size 无效: %d", blockSize)
	}
	if blockSize > maxInt {
		return driveMultipartSession{}, fmt.Errorf("upload_prepare 返回的 block_size 超出可分配上限")
	}
	if blockNum <= 0 {
		return driveMultipartSession{}, fmt.Errorf("upload_prepare 返回的 block_num 无效: %d", blockNum)
	}
	expected, err := expectedMultipartBlockNum(payloadSize, blockSize)
	if err != nil {
		return driveMultipartSession{}, err
	}
	if blockNum != expected {
		return driveMultipartSession{}, fmt.Errorf("upload_prepare 返回的分片计划不一致: block_size=%d, block_num=%d, expected=%d, size=%d",
			blockSize, blockNum, expected, payloadSize)
	}
	return driveMultipartSession{
		UploadID:  uploadID,
		BlockSize: blockSize,
		BlockNum:  blockNum,
	}, nil
}

func parseMultipartSessionFromAPI(raw []byte, payloadSize int64) (driveMultipartSession, error) {
	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UploadID  string `json:"upload_id"`
			BlockSize any    `json:"block_size"`
			BlockNum  any    `json:"block_num"`
		} `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&apiResp); err != nil {
		return driveMultipartSession{}, fmt.Errorf("解析 upload_prepare 响应失败: %w", err)
	}
	if apiResp.Code != 0 {
		return driveMultipartSession{}, fmt.Errorf("初始化分片上传失败: code=%d, msg=%s", apiResp.Code, apiResp.Msg)
	}
	maxInt := maxNativeInt64()
	blockSize, ok := jsonNumberAsPositiveInt64(apiResp.Data.BlockSize, maxInt)
	if !ok {
		return driveMultipartSession{}, fmt.Errorf("upload_prepare 返回的 block_size 超出可分配上限或无效")
	}
	blockNum, ok := jsonNumberAsPositiveInt64(apiResp.Data.BlockNum, maxInt)
	if !ok {
		return driveMultipartSession{}, fmt.Errorf("upload_prepare 返回的 block_num 无效")
	}
	return validateMultipartSession(apiResp.Data.UploadID, blockSize, blockNum, payloadSize)
}
