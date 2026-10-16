import DOMPurify from 'dompurify'
import { getImageUrl } from './image'

/**
 * 将 HTML 内容中的图片路径转换为显示用的绝对路径
 * /uploads/xxx -> http://domain/uploads/xxx
 */
export function processHtmlForDisplay(html: string): string {
    if (!html) return ''

    // 匹配 src="/uploads/..."，支持单引号和双引号
    // 使用非贪婪匹配 .*?
    return html.replace(/src=["'](\/uploads\/.*?)["']/g, (_, path) => {
        return `src="${getImageUrl(path)}"`
    })
}

/**
 * 富文本正文的白名单。覆盖后台 Tiptap 编辑器的全部产出：段落、标题、列表、
 * 引用、代码、链接、图片、表格（Table/TableRow/TableHeader/TableCell），
 * 以及 TextAlign 与 Color 产生的行内 style。
 *
 * 刻意不放行的：script / iframe / object / embed / form / input 等可执行或
 * 可提交外部请求的标签，以及 id / name（防止 DOM clobbering）。
 */
export const RICH_TEXT_SANITIZE_CONFIG = {
    ALLOWED_TAGS: [
        'p', 'br', 'hr', 'strong', 'em', 'u', 's', 'del', 'code', 'pre', 'blockquote',
        'ul', 'ol', 'li', 'a', 'img', 'span', 'div',
        'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
        'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td', 'colgroup', 'col',
    ],
    ALLOWED_ATTR: [
        'href', 'target', 'rel', 'src', 'alt', 'title', 'width', 'height',
        'style', 'colspan', 'rowspan', 'align',
    ],
    FORBID_ATTR: ['id', 'name', 'onerror', 'onload', 'onclick'],
    ALLOW_DATA_ATTR: false,
    // 刻意不设 ALLOWED_URI_REGEXP：DOMPurify 会把这个正则套用到**每一个**属性值上，
    // 而不只是 URL 属性。colspan="2"、rowspan="1" 这类非 URL 取值无法匹配 URI 模式，
    // 会被整体丢弃，导致表格布局在净化后消失。DOMPurify 内置的 URI 处理已经会剥除
    // javascript: 与 data: 等可执行协议（大小写混淆同样能挡住），无需再自定义。
}

// DOMPurify 允许 style 属性，但 style 里能藏 expression() 之类的旧式执行向量，
// 也会被用来做点击劫持式的覆盖布局。这里挂一个钩子把样式收敛到编辑器真正会产出的
// 声明上（对齐、文字颜色、背景色），其余一律丢弃。钩子只需注册一次。
//
// 同一个钩子顺带剥掉 data: URI。DOMPurify 默认对 img 等标签放行 data:，其中
// data:image/svg+xml 可内嵌 SVG；而服务端 bluemonday 只放行 http/https/mailto/tel。
// 无法用配置项关掉——DATA_URI_TAGS 只有 ADD_DATA_URI_TAGS 这个追加入口，没有移除
// 入口——因此只能在钩子里处理。
let sanitizeHookRegistered = false

export const ensureRichTextStyleHook = (): void => {
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

        const style = element.getAttribute('style')
        if (style === null) return

        const safeDeclarations: string[] = []
        for (const declaration of style.split(';')) {
            const separator = declaration.indexOf(':')
            if (separator === -1) continue

            const property = declaration.slice(0, separator).trim().toLowerCase()
            const value = declaration.slice(separator + 1).trim()
            if (!property || !value) continue

            // 只保留排版类声明；值里出现 url( 或 \ 说明可能是构造出来的绕过载荷。
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
 * 净化来自后端的富文本 HTML，供 v-html 直接渲染。
 *
 * 顺序是「先处理图片路径，再净化」：图片路径重写会把 src 换成本站绝对地址，
 * 净化放在最后可以保证没有任何一步在净化之后重新引入内容。
 *
 * 用途：商品详情正文、博客正文、条款/隐私政策等一切由后台编辑器产出、
 * 又被前端 v-html 渲染的内容。未净化直接渲染会让内容编辑者（不必是超管）
 * 通过正文注入脚本，进而读走同源 localStorage 里的 user_token / admin_token。
 */
export function sanitizeRichTextHtml(html: string | null | undefined): string {
    if (html === null || html === undefined) return ''

    const raw = String(html)
    if (raw.trim() === '') return ''

    ensureRichTextStyleHook()

    // 走 DOM fragment 而不是字符串：补链接属性时改 DOM 比用正则改 HTML 安全，
    // 不会因为拼接字符串反而引入新的注入面。
    const fragment = DOMPurify.sanitize(processHtmlForDisplay(raw), {
        ...RICH_TEXT_SANITIZE_CONFIG,
        RETURN_DOM_FRAGMENT: true,
    }) as unknown as DocumentFragment | null

    // RETURN_DOM_FRAGMENT 下，若 DOMPurify 无法构造解析文档会返回 null（例如
    // <frameset> 这类不能作为文档根的输入）。不判空就会在 querySelectorAll 上抛错，
    // 而调用点在 computed 里，异常会直接冒泡到渲染、白屏整页。
    if (!fragment) return ''

    hardenExternalLinks(fragment)

    const holder = document.createElement('div')
    holder.appendChild(fragment)
    return holder.innerHTML
}

/**
 * 将 HTML 内容中的图片路径转换为存储用的相对路径
 * http://domain/uploads/xxx -> /uploads/xxx
 */
export function processHtmlForStorage(html: string): string {
    if (!html) return ''

    const apiBaseUrl = import.meta.env.VITE_API_BASE_URL || ''
    let apiHost = ''
    try {
        if (apiBaseUrl) {
            // 提取域名 (例如 localhost:8080 或 domain.com)
            apiHost = new URL(apiBaseUrl).host
        } else {
            // 如果未配置 API_BASE_URL，则认为是同源，使用当前域名
            apiHost = window.location.host
        }
    } catch (e) {
        // Fallback
        apiHost = window.location.host
    }

    // 匹配 src="value" 或 src='value'
    return html.replace(/src=["'](.*?)["']/g, (match, src) => {
        try {
            // 如果是完整 URL (http:// 或 https://)
            if (src.startsWith('http://') || src.startsWith('https://')) {
                const url = new URL(src)

                // 检查域名是否匹配 API 域名，且路径以 /uploads/ 开头
                // url.host 会自动处理端口号 (例如 localhost:8080)
                if (url.host === apiHost && url.pathname.startsWith('/uploads/')) {
                    // 返回相对路径，例如 src="/uploads/xxx.png"
                    // 始终标准化为双引号
                    return `src="${url.pathname}"`
                }
            }
        } catch (e) {
            // URL 解析失败，忽略，返回原样
        }
        return match
    })
}
