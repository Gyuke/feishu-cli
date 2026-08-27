package client

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildPresentationXML_NamespaceHTTPS 验证 SML namespace 必须使用 https。
func TestBuildPresentationXML_NamespaceHTTPS(t *testing.T) {
	xml := buildPresentationXML("测试标题 & <Deck>", 1920, 1080)

	wantNamespace := `xmlns="https://www.larkoffice.com/sml/2.0"`
	if !strings.Contains(xml, wantNamespace) {
		t.Errorf("XML 必须包含 HTTPS namespace %q, 实际得到:\n%s", wantNamespace, xml)
	}

	// 严禁 HTTP
	if strings.Contains(xml, `xmlns="http://www.larkoffice.com/sml/2.0"`) {
		t.Errorf("XML 不得使用 HTTP namespace: %s", xml)
	}

	if !strings.Contains(xml, `<title>测试标题 &amp; &lt;Deck&gt;</title>`) {
		t.Errorf("XML 转义不正确: %s", xml)
	}
	if !strings.Contains(xml, `width="1920"`) || !strings.Contains(xml, `height="1080"`) {
		t.Errorf("XML 宽高属性不正确: %s", xml)
	}
}

// TestSlidesMediaParentType 覆盖 slides media parent_type 选择：
// 普通 slides 用 slide_file，导入型 office deck (fake_office_ 前缀) 用 office_slide_file。
func TestSlidesMediaParentType(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"原生 slides presentation_id", "zTqAwsEb4clrjOLd3drAcNZabcef", "slide_file"},
		{"普通 short token", "sldcnABC123", "slide_file"},
		{"导入 office deck token", "fake_office_ppt_123456", "office_slide_file"},
		{"仅 fake_office_ 前缀", "fake_office_", "office_slide_file"},
		{"前缀在中间不匹配", "my_fake_office_deck", "slide_file"},
		{"空 token 兜底 slide_file", "", "slide_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slidesMediaParentType(tc.token)
			if got != tc.want {
				t.Errorf("slidesMediaParentType(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

// TestUploadSlidesMedia_ParentTypeSelection 验证 UploadSlidesMedia 在 wire 层发送正确的 parent_type。
func TestUploadSlidesMedia_ParentTypeSelection(t *testing.T) {
	var gotParentType string
	var gotParentNode string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err == nil && strings.HasPrefix(mediaType, "multipart/") {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					break
				}
				formName := p.FormName()
				data, _ := io.ReadAll(p)
				if formName == "parent_type" {
					gotParentType = string(data)
				}
				if formName == "parent_node" {
					gotParentNode = string(data)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"file_token":"boxcn_media_token_123"}}`)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)

	// 创建临时图片文件
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "test.png")
	if err := os.WriteFile(imgPath, []byte("fake png content"), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. 原生 Slides deck 上传
	gotParentType = ""
	gotParentNode = ""
	token, err := UploadSlidesMedia(imgPath, "test.png", "zTqAwsEb4clrjOLd3drAcNZabcef", "u-test")
	if err != nil {
		t.Fatalf("UploadSlidesMedia 失败: %v", err)
	}
	if token != "boxcn_media_token_123" {
		t.Errorf("返回 token 不符: %s", token)
	}
	if gotParentType != "slide_file" {
		t.Errorf("原生 deck parent_type 应为 slide_file，实际为 %s", gotParentType)
	}
	if gotParentNode != "zTqAwsEb4clrjOLd3drAcNZabcef" {
		t.Errorf("parent_node 不符: %s", gotParentNode)
	}

	// 2. 导入型 Office deck 上传
	gotParentType = ""
	gotParentNode = ""
	token, err = UploadSlidesMedia(imgPath, "test.png", "fake_office_deck_999", "u-test")
	if err != nil {
		t.Fatalf("UploadSlidesMedia 失败: %v", err)
	}
	if gotParentType != "office_slide_file" {
		t.Errorf("Office deck parent_type 应为 office_slide_file，实际为 %s", gotParentType)
	}
	if gotParentNode != "fake_office_deck_999" {
		t.Errorf("parent_node 不符: %s", gotParentNode)
	}
}

// TestGetSlides 验证 GetSlides 读取演示文稿 XML。
func TestGetSlides(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"code": 0,
			"msg": "ok",
			"data": {
				"xml_presentation": {
					"content": "<presentation xmlns=\"https://www.larkoffice.com/sml/2.0\"><title>My Presentation</title></presentation>",
					"presentation_id": "pres_123",
					"revision_id": 5
				}
			}
		}`)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)

	res, err := GetSlides("pres_123", 0, "u-test")
	if err != nil {
		t.Fatalf("GetSlides 意外报错: %v", err)
	}
	if gotPath != "/open-apis/slides_ai/v1/xml_presentations/pres_123" {
		t.Errorf("请求路径不符: %s", gotPath)
	}
	if res.XmlPresentationID != "pres_123" {
		t.Errorf("presentation_id 不符: %s", res.XmlPresentationID)
	}
	if res.RevisionID != 5 {
		t.Errorf("revision_id 不符: %d", res.RevisionID)
	}
	if !strings.Contains(res.Content, "My Presentation") {
		t.Errorf("content 不符: %s", res.Content)
	}
}

// TestCreateSlides 验证 CreateSlides 创建演示文稿。
func TestCreateSlides(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"code": 0,
			"msg": "ok",
			"data": {
				"xml_presentation_id": "new_pres_999",
				"revision_id": 1
			}
		}`)
	}))
	t.Cleanup(srv.Close)
	setupTestConfig(t, srv.URL)

	res, err := CreateSlides(CreateSlidesOptions{
		Title:           "Quarterly Report",
		Width:           1920,
		Height:          1080,
		UserAccessToken: "u-test",
	})
	if err != nil {
		t.Fatalf("CreateSlides 失败: %v", err)
	}
	if res.XmlPresentationID != "new_pres_999" {
		t.Errorf("xml_presentation_id 不符: %s", res.XmlPresentationID)
	}

	var reqBody struct {
		XmlPresentation struct {
			Content string `json:"content"`
		} `json:"xml_presentation"`
	}
	if err := json.Unmarshal([]byte(gotBody), &reqBody); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	if !strings.Contains(reqBody.XmlPresentation.Content, `xmlns="https://www.larkoffice.com/sml/2.0"`) {
		t.Errorf("创建请求体 XML 应包含 HTTPS namespace: %s", reqBody.XmlPresentation.Content)
	}
}
