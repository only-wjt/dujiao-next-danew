import DOMPurify from 'dompurify'

/**
 * 后台富文本白名单。
 *
 * 与前台的 RICH_TEXT_SANITIZE_CONFIG 相比，这里额外放行 Telegram 的 HTML 子集：
 * 当前 admin 侧只有 Telegram 广播预览一处渲染富文本，而广播正文是 Telegram
 * markup 而非网页 markup——编辑器提示词明确写的是「支持 Telegram HTML，如
 * <b>加粗</b>」。若沿用网页白名单，预览里的 <b>/<i> 会被剥成纯文本，
 * tg-spoiler 会失效，预览与真正发出去的内容不一致。
 *
 * 注意广播正文在服务端是按 Telegram markup 原样存储、原样转发给 Telegram API 的
 * （ParseMode: HTML），不做网页净化——两套标记语言的白名单不能混用。
 *
 * 刻意不放行 script / iframe / object / embed / form / input 等可执行或可提交
 * 外部请求的标签，以及 id / name（防止 DOM clobbering）。
 */
export const RICH_TEXT_SANITIZE_CONFIG = {
  ALLOWED_TAGS: [
    'p', 'br', 'hr', 'strong', 'b', 'em', 'i', 'u', 'ins', 's', 'strike', 'del',
    'code', 'pre', 'blockquote',
    'ul', 'ol', 'li', 'a', 'img', 'span', 'div',
    'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
    'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td', 'colgroup', 'col',
    // Telegram 专有标签
    'tg-spoiler', 'tg-emoji',
  ],
  ALLOWED_ATTR: [
    'href', 'target', 'rel', 'src', 'alt', 'title', 'width', 'height',
    'style', 'colspan', 'rowspan', 'align',
    // Telegram：<span class="tg-spoiler"> 与 <tg-emoji emoji-id="...">
    'class', 'emoji-id',
  ],
  FORBID_ATTR: ['id', 'name', 'onerror', 'onload', 'onclick'],
  ALLOW_DATA_ATTR: false,
  // 刻意不设 ALLOWED_URI_REGEXP：DOMPurify 会把这个正则套用到**每一个**属性值上，
  // 而不只是 URL 属性。colspan="2"、rowspan="1" 这类非 URL 取值无法匹配 URI 模式，
  // 会被整体丢弃，导致表格布局在净化后消失。DOMPurify 内置的 URI 处理已经会剥除
  // javascript: 与 data: 等可执行协议（大小写混淆同样能挡住），无需再自定义。
}

// style 属性是编辑器排版所必需的（对齐、文字颜色），但它也能承载 expression()
// 这类旧式执行向量。这里挂钩子把声明收敛到编辑器真正会产出的几项。
//
// 同一个钩子顺带剥掉 data: URI。DOMPurify 默认对 img 等标签放行 data:，其中
// data:image/svg+xml 可内嵌 SVG；而服务端 bluemonday 只放行 http/https/mailto/tel。
// 无法用配置项关掉——DATA_URI_TAGS 只有 ADD_DATA_URI_TAGS 这个追加入口，没有移除
// 入口——因此只能在钩子里处理。
let sanitizeHookRegistered = false

const ensureSanitizeHook = () => {
  if (sanitizeHookRegistered) return
  sanitizeHookRegistered = true

  DOMPurify.addHook('afterSanitizeAttributes', (node) => {
    const element = node as Element
    if (typeof element.getAttribute !== 'function') return

    // data: URI 一律剥除，与服务端策略对齐。
    for (const attr of ['src', 'href', 'xlink:href']) {
      const value = element.getAttribute(attr)
      if (value && /^\s*data:/i.test(value)) {
        element.removeAttribute(attr)
      }
    }

    // class 仅为 Telegram 的 <span class="tg-spoiler"> 而放行，其余取值一概剥除：
    // 任意 class 会命中站内样式表，属于不必要的攻击面。
    const className = element.getAttribute('class')
    if (className !== null && className.trim() !== 'tg-spoiler') {
      element.removeAttribute('class')
    }

    const style = element.getAttribute('style')
    if (style === null) return

    const safeDeclarations: string[] = []
    for (const declaration of style.split(';')) {
      const separator = declaration.indexOf(':')
      if (separator === -1) continue

      const property = declaration.slice(0, separator).trim().toLowerCase()
      const value = declaration.slice(separator + 1).trim()
      if (!property || !value) continue

      if (property !== 'text-align' && property !== 'color' && property !== 'background-color') continue
      if (/url\(|\\|expression/i.test(value)) continue

      safeDeclarations.push(`${property}: ${value}`)
    }

    if (safeDeclarations.length > 0) {
      element.setAttribute('style', safeDeclarations.join('; '))
    } else {
      element.removeAttribute('style')
    }
  })
}

// 外链统一补 target/rel。DOMPurify 不会自动加这两项（与 Go 侧的 bluemonday 不同），
// 不补则外链在当前标签页打开，配合 window.opener 可被反向控制（tabnabbing）。
//
// 必须同时覆盖协议相对地址（//host/path）：它同样是跨域外链，但 /^https?:\/\// 匹配
// 不到，只按绝对地址判断会漏掉它，攻击者预先写好的 rel="opener" 就被保留下来。
const EXTERNAL_URL = /^(?:https?:)?\/\//i

const hardenExternalLinks = (fragment: DocumentFragment): void => {
  fragment.querySelectorAll('a[href]').forEach((anchor) => {
    const href = anchor.getAttribute('href') || ''
    if (!EXTERNAL_URL.test(href)) {
      // 站内链接：去掉内容里自带的 target/rel，避免用 rel="opener" 之类削弱默认防护。
      anchor.removeAttribute('target')
      anchor.removeAttribute('rel')
      return
    }

    anchor.setAttribute('target', '_blank')
    anchor.setAttribute('rel', 'noopener noreferrer')
  })
}

/**
 * 净化后台侧要交给 v-html 的富文本 HTML。
 *
 * 后台与前台同源，localStorage 里的 admin_token 可被任意同源脚本读走，
 * 因此后台渲染这类内容同样必须净化——广播正文、富文本预览等均适用。
 */
export function sanitizeRichTextHtml(html: string | null | undefined): string {
  if (html === null || html === undefined) return ''

  const raw = String(html)
  if (raw.trim() === '') return ''

  ensureSanitizeHook()

  // 走 DOM fragment 而不是字符串：补链接属性时改 DOM 比用正则改 HTML 安全，
  // 不会因为拼接字符串反而引入新的注入面。
  const fragment = DOMPurify.sanitize(raw, {
    ...RICH_TEXT_SANITIZE_CONFIG,
    RETURN_DOM_FRAGMENT: true,
  }) as unknown as DocumentFragment | null

  // RETURN_DOM_FRAGMENT 下，若 DOMPurify 无法构造解析文档会返回 null（例如
  // <frameset> 这类不能作为文档根的输入）。不判空就会在 querySelectorAll 上抛错，
  // 而这个函数的调用点在 computed 里，异常会直接冒泡到渲染、白屏整页。
  if (!fragment) return ''

  hardenExternalLinks(fragment)

  const holder = document.createElement('div')
  holder.appendChild(fragment)
  return holder.innerHTML
}
