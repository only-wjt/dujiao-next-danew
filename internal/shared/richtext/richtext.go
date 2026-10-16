// Package richtext 负责把后台富文本编辑器产出的 HTML 收敛到一份安全白名单。
//
// 为什么需要服务端净化：商品详情、博客正文等内容会被前端以 v-html 直接渲染。
// 前端虽然在渲染处也做了净化（frontend/user/src/utils/content.ts），但那只保护
// 本仓库自带的前端——内容还会经 /api/v1/upstream/products 分发给下游站点，
// 由不受本仓库控制的第三方前端渲染。写入侧净化是让存储本身安全，不依赖任何
// 单个渲染方是否记得净化。
//
// 白名单与前端 RICH_TEXT_SANITIZE_CONFIG 保持一致，覆盖后台 Tiptap 编辑器的
// 全部产出：段落、标题、列表、引用、代码、链接、图片、表格，以及 TextAlign 与
// Color 产生的行内排版样式。
package richtext

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// 颜色取值的形状白名单。刻意不用「禁止若干子串」的黑名单写法：
// 那种正则很容易被宽松字符类吸收（例如用 [^\\]* 打头尾，url(...) 会被直接吞掉），
// 看起来在过滤、实际全部放行。这里只接受完整的合法形状，其余一律拒绝。
var (
	hexColorPattern   = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	funcColorPattern  = regexp.MustCompile(`^(?:rgb|rgba|hsl|hsla)\(\s*[0-9.,%\s/]+\)$`)
	namedColorPattern = regexp.MustCompile(`^[a-zA-Z]{3,20}$`)
)

// isSafeColorValue 判断颜色声明是否只由合法字符构成。
// 注意 funcColorPattern 要求以 rgb/rgba/hsl/hsla 开头，因此
// url(https://...) 既不匹配颜色函数、也因含括号与斜杠而不匹配颜色名，会被拒绝。
func isSafeColorValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return hexColorPattern.MatchString(value) ||
		funcColorPattern.MatchString(value) ||
		namedColorPattern.MatchString(value)
}

var policy = buildPolicy()

func buildPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// 标签白名单。刻意不含 script / iframe / object / embed / form / input /
	// textarea / button 等可执行或可提交外部请求的标签。
	p.AllowElements(
		"p", "br", "hr", "strong", "b", "em", "i", "u", "s", "del", "ins", "mark",
		"code", "pre", "kbd", "samp", "var", "blockquote", "abbr", "cite", "small", "sub", "sup",
		"ul", "ol", "li", "dl", "dt", "dd",
		"a", "img", "span", "div",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"table", "thead", "tbody", "tfoot", "tr", "th", "td", "caption", "colgroup", "col",
	)

	// 链接：只放行 http(s)/mailto/tel 与站内相对路径；挡掉 javascript:、data: 等。
	p.AllowURLSchemes("http", "https", "mailto", "tel")
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(true)

	// 外部链接统一补 rel，防止 target=_blank 的反向标签劫持。
	p.RequireNoFollowOnLinks(false)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("target", "rel").OnElements("a")

	// 图片：src 走同一套 URL 约束，只允许 alt/title/尺寸类属性。
	p.AllowAttrs("src").OnElements("img")
	p.AllowAttrs("alt", "title", "width", "height").OnElements("img")

	// 表格与列表的布局属性；colwidth 是 Tiptap 表格列宽的往返依据。
	p.AllowAttrs("colspan", "rowspan", "align", "colwidth").OnElements("th", "td", "col")
	p.AllowAttrs("align").OnElements("table", "tr", "p", "div")

	// 行内样式只保留编辑器会产出的排版声明：对齐用枚举，颜色用形状白名单。
	p.AllowStyles("text-align").Matching(regexp.MustCompile(`(?i)^(left|right|center|justify)$`)).Globally()
	p.AllowStyles("color", "background-color").MatchingHandler(isSafeColorValue).Globally()

	return p
}

// Sanitize 净化一段富文本 HTML。空输入返回空串。
//
// 传入的内容若不是 HTML（纯文本），同样可以安全通过：不会有标签被保留，
// 其中形如 "<" 的字符会被转义，因此调用方无需区分两种形态。
func Sanitize(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}
	return policy.Sanitize(html)
}

// SanitizeLocalized 对「语言码 -> HTML」的多语言字段整体净化。
//
// 商品 content、文章 content 等字段都是这种结构；非字符串取值（历史数据里可能
// 混入 null 或数字）原样保留，交由调用方既有的归一化逻辑处理。
func SanitizeLocalized(block map[string]interface{}) map[string]interface{} {
	if len(block) == 0 {
		return block
	}

	normalized := make(map[string]interface{}, len(block))
	for key, value := range block {
		raw, ok := value.(string)
		if !ok {
			normalized[key] = value
			continue
		}
		normalized[key] = Sanitize(raw)
	}
	return normalized
}
