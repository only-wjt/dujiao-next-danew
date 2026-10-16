import { describe, expect, it } from 'vitest'
import { RICH_TEXT_SANITIZE_CONFIG, sanitizeRichTextHtml } from './sanitizeHtml'

// 商品详情、博客正文、条款政策、Telegram 广播等富文本都会被 v-html 直接渲染。
// 后台与前台同源，localStorage 里的 user_token / admin_token 可被任意同源脚本读走，
// 因此内容编辑者（不必是超管）只要能往正文里注入脚本，就能劫持浏览该内容的用户
// 甚至管理员。
//
// 这批用例的作用是防回归：以后有人放宽白名单、误删 FORBID_ATTR、
// 或升级 DOMPurify 引入行为变化时，这里会立刻红。
describe('sanitizeRichTextHtml', () => {
  const attacks: Array<[name: string, payload: string]> = [
    ['script 标签', '<p>正文</p><script>alert(localStorage.admin_token)</script>'],
    ['img onerror', '<img src=x onerror=alert(localStorage.user_token)>'],
    ['大小写混淆事件属性', '<IMG SRC=x OnErRoR=alert(1)>'],
    ['svg onload', '<svg onload=alert(1)></svg>'],
    ['svg 嵌套 script', '<svg><script>alert(1)</script></svg>'],
    ['iframe', '<iframe src="https://evil.example"></iframe>'],
    ['iframe srcdoc', '<iframe srcdoc="<script>alert(1)</script>"></iframe>'],
    ['a javascript:', '<a href="javascript:alert(1)">click</a>'],
    ['a 大小写混淆协议', '<a href="JaVaScRiPt:alert(1)">click</a>'],
    ['form + formaction', '<form action=x><button formaction=javascript:alert(1)>x</button></form>'],
    ['body onload', '<body onload=alert(1)>'],
    ['img data URI', '<img src="data:text/html,<script>alert(1)</script>">'],
    ['style url 外链', '<div style="background-color: url(https://evil.example/a.png)">x</div>'],
    ['style expression', '<div style="width: expression(alert(1))">x</div>'],
    ['object 标签', '<object data="https://evil.example"></object>'],
    ['embed 标签', '<embed src="https://evil.example">'],
    ['base 标签', '<base href="https://evil.example/">'],
    ['meta refresh', '<meta http-equiv="refresh" content="0;url=https://evil.example">'],
  ]

  it.each(attacks)('剥除攻击向量：%s', (_name, payload) => {
    const output = sanitizeRichTextHtml(payload)

    for (const forbidden of [
      '<script', '<iframe', '<object', '<embed', '<form', '<meta', '<base',
      'onerror', 'onload', 'onclick', 'javascript:', 'data:text/html', 'expression(',
    ]) {
      expect(output.toLowerCase()).not.toContain(forbidden)
    }
  })

  it('丢弃通过 id / name 做 DOM clobbering 的属性', () => {
    const output = sanitizeRichTextHtml('<div id="config" name="config">x</div>')

    expect(output).not.toContain('id=')
    expect(output).not.toContain('name=')
  })

  it('保留编辑器产出的正常排版', () => {
    const html = [
      '<h2>规格</h2>',
      '<p style="text-align: center">居中说明</p>',
      '<p><strong>加粗</strong><em>斜体</em><u>下划线</u></p>',
      '<ul><li>项目一</li><li>项目二</li></ul>',
      '<blockquote>引用</blockquote>',
      '<pre><code>code()</code></pre>',
      '<table><thead><tr><th colspan="2">表头</th></tr></thead>',
      '<tbody><tr><td rowspan="1">单元格</td><td>另一个</td></tr></tbody></table>',
      '<p><img src="/uploads/2026/09/a.png" alt="图" title="标题"></p>',
    ].join('')

    const output = sanitizeRichTextHtml(html)

    for (const kept of [
      '<h2>', '<strong>', '<em>', '<u>', '<ul>', '<li>', '<blockquote>',
      '<pre>', '<code>', '<table>', '<thead>', '<th', '<td',
      'colspan', 'rowspan', 'text-align',
      'src="/uploads/2026/09/a.png"', 'alt="图"',
    ]) {
      expect(output).toContain(kept)
    }
  })

  it('外链自动补 rel，避免 target=_blank 的反向标签劫持', () => {
    const output = sanitizeRichTextHtml('<a href="https://example.com">外链</a>')

    expect(output).toContain('target="_blank"')
    expect(output).toMatch(/rel="[^"]*(noopener|noreferrer)/)
  })

  it('空输入返回空串，调用方无需判空', () => {
    expect(sanitizeRichTextHtml('')).toBe('')
    expect(sanitizeRichTextHtml('   ')).toBe('')
    expect(sanitizeRichTextHtml(null)).toBe('')
    expect(sanitizeRichTextHtml(undefined)).toBe('')
  })

  it('保留 Telegram 广播预览所需的标记', () => {
    // 广播正文是 Telegram markup 而非网页 markup，编辑器提示词明确写的是
    // <b>加粗</b>。若沿用纯网页白名单，预览会把 <b>/<i> 剥成纯文本、
    // 令 tg-spoiler 失效，与真正发出去的内容不一致。
    for (const kept of [
      '<b>加粗</b>',
      '<i>斜体</i>',
      '<code>c</code>',
      '<tg-spoiler>剧透</tg-spoiler>',
      '<span class="tg-spoiler">剧透</span>',
    ]) {
      expect(sanitizeRichTextHtml(kept)).toBe(kept)
    }
  })

  it('class 仅放行 tg-spoiler，其余取值一概剥除', () => {
    // 任意 class 会命中站内样式表，是不必要的攻击面。
    for (const payload of [
      '<span class="overlay">x</span>',
      '<div class="fixed inset-0 bg-black">x</div>',
      '<p class="hidden">x</p>',
    ]) {
      expect(sanitizeRichTextHtml(payload)).not.toContain('class=')
    }
  })

  it('data: URI 一律剥除，与服务端策略对齐', () => {
    // DOMPurify 默认对 img 放行 data:，服务端 bluemonday 只放行 http/https/mailto/tel。
    // 该配置项只有 ADD_DATA_URI_TAGS 追加入口、没有移除入口，故由钩子处理。
    expect(sanitizeRichTextHtml('<img src="data:image/svg+xml,<svg onload=alert(1)>">')).not.toContain('data:')
    expect(sanitizeRichTextHtml('<img src="da&#116;a:image/svg+xml,<svg onload=alert(1)>">')).not.toContain('data:')
    expect(sanitizeRichTextHtml('<img src="/uploads/ok.png">')).toContain('/uploads/ok.png')
  })

  it('协议相对地址同样会被加固，站内链接则去掉自带的 target/rel', () => {
    // /^https?:\/\// 匹配不到 //evil.example，只按绝对地址判断会漏掉它，
    // 攻击者预先写好的 rel="opener" 就被保留下来。
    const external = sanitizeRichTextHtml('<a href="//evil.example" rel="opener">x</a>')
    expect(external).toContain('rel="noopener noreferrer"')
    expect(external).not.toContain('rel="opener"')

    const internal = sanitizeRichTextHtml('<a href="/admin" target="_blank" rel="opener">x</a>')
    expect(internal).not.toContain('target=')
    expect(internal).not.toContain('rel=')
  })

  it('无法构造解析文档时不抛错，返回空串', () => {
    // RETURN_DOM_FRAGMENT 下 DOMPurify 可能返回 null（<frameset> 不能作为文档根）。
    // 不判空会在 querySelectorAll 上抛错，而调用点在 computed 内，异常会冒泡到
    // 渲染、白屏整页。
    for (const payload of [
      '<frameset><frame></frameset>',
      '<frameset cols="1"><frame src="https://evil.example"></frameset>',
    ]) {
      expect(() => sanitizeRichTextHtml(payload)).not.toThrow()
      expect(sanitizeRichTextHtml(payload)).toBe('')
    }
  })

  it('白名单不含任何可执行或可提交外部请求的标签', () => {
    const allowed = RICH_TEXT_SANITIZE_CONFIG.ALLOWED_TAGS

    for (const dangerous of ['script', 'iframe', 'object', 'embed', 'form', 'input', 'button', 'base', 'meta', 'style', 'link']) {
      expect(allowed).not.toContain(dangerous)
    }
  })
})
