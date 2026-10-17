# 实时发码改接 danew_cdk 设计（cdkissue）

> 日期：2026-10-17　·　状态：**待确认后实现**（本文只定设计，不改代码）
> 接口契约以 danew_cdk 仓库 `docs/卡网发码接口设计-2026-10-17.md` §4 为准，本文只写卡网侧改造。

## 0. 目标

- 实时发码从「直接调 ZovoCard」改为「调 danew_cdk 卡网接口」，码进 danew_cdk 库，买家在 cdk.danew.cc 能查单、兑换；
- **ZovoCard 直连实现保留**，作为第二个 provider，后台可切回；
- 订单、交付、退款、补发扫描等外层流程不变：`RealtimeIssuer` / `OrderabilityChecker` 端口签名不动。

## 1. 现状要点（改造依据）

| 位置 | 现状 |
|---|---|
| `domain/binding.go` `ApplyItemSnapshot` | 下单时把 `provider=zovocard, plan, payment_country` 写进订单项快照；`ReadItemSnapshot` 不读 provider |
| `domain/request.go` `IssueRequest.Provider` | 字段已有，恒写 `zovocard` |
| `domain/config.go` | 每个 provider 一行配置（`provider` 唯一索引）；`BaseURL()` 按环境写死为 zovocard.com；`Configured()` 要求 `ak_` App ID |
| `application/issue.go` `EnsureIssued` | 单一 `resolveCredentials` 闭包、单一 `gateway` |
| `application/issue_call.go` | 瞬时错误同键重试，`MaxAutoAttempts=8` 后转 `unknown`；张数不符转 `unknown` |
| `contract/errors.go` `ClassifyHTTPStatus` | 409/429/5xx 视为结果待定，其余 4xx 明确拒绝 |
| `infrastructure/zovocard/client.go` | `Issue` / `Overview` / `DisableCDKs` 实现 `Gateway` |
| 后台 | `CdkIssueSettings.vue`（ZovoCard 配置）、`CdkIssueRequests.vue`（发码记录） |

danew_cdk 接口的错误语义和 ZovoCard 一致（明确拒绝 = 4xx，结果待定 = 409/429/5xx，同键重放返回同一批码），所以**重试状态机完全不用改**，只需要多一个网关实现。

## 2. 方案总览

```
                ┌──────────── Service（编排不变：建单 / 占用 / 重试 / 交付 / 退款）
                │
 Gateway 注册表 ├─ zovocard   → infrastructure/zovocard（保留）
 (按 provider)  └─ danew_cdk  → infrastructure/danewcdk（新增）
```

- 新增常量 `ProviderDanewCDK = "danew_cdk"`；
- **同一时刻只有一个 provider 启用**，即当前发码 provider；
- 下单时把当前 provider 写进订单项快照；建发码请求时 `IssueRequest.Provider` 取快照值；之后的重试、停用**一律按请求上记录的 provider 走**，切换后台配置不影响在途订单。

## 3. 领域层

### 3.1 `domain/config.go`

| 改动 | 说明 |
|---|---|
| 新增列 `APIBaseURL varchar(255)` | 仅 `danew_cdk` 用，例如 `https://cdk.danew.cc/api/v1/cardnet` |
| `BaseURL()` | `zovocard`：保持按环境写死；`danew_cdk`：返回 `APIBaseURL` |
| `Configured()` | `zovocard`：`ak_` App ID + Secret（不变）；`danew_cdk`：`APIBaseURL` + Secret |
| `DefaultConfig(provider)` | 按 provider 返回默认值；`danew_cdk` 无环境概念 |

`APIBaseURL` 校验：必须 `https://`，去掉末尾 `/`，不允许 query/fragment，拒绝 IP 字面量与 `localhost`。

Secret 仍存 `AppSecretEncrypted`；`danew_cdk` 要求 `cn_live_` 前缀，`SecretHint` 识别该前缀。

### 3.2 `domain/binding.go`

- `ApplyItemSnapshot(snapshot, provider, plan, country)` 增加 provider 参数；
- `ReadItemSnapshot` 返回 `(provider, plan, country, ok)`；**快照里没有 provider 时视为 `zovocard`**，历史订单补发仍打 ZovoCard；
- 商品绑定不再决定 provider（provider 是全局的），`Binding.Provider` 列只作记录。

### 3.3 `domain/request.go`

- 幂等键格式不变：`dj-{orderNo}-{itemID}-{chunk}[-rN]`；danew_cdk 按「客户端 + 键」隔离，与 ZovoCard 互不影响；
- `MaxCodesPerRequest = 200` 不变，与 danew_cdk 单次上限一致；
- 新增列 `ProviderRequestID varchar(64)`：保存 danew_cdk 返回的 `request_id`，两边对账用。

### 3.4 `domain/overview.go`

`Overview` 增加两个可选字段，ZovoCard 网关不填：

```go
Issuable *bool         `json:"issuable,omitempty"`
Client   *ClientLimits `json:"client,omitempty"` // allowed_plans / allowed_countries / daily_code_limit / issued_today
```

## 4. 应用层

### 4.1 多 Gateway

```go
type Options struct {
    ...
    Gateways map[string]cdkissuecontract.Gateway // provider → gateway
}
```

- 至少注册 `zovocard`；`s.gatewayFor(provider)` 取不到时按「未配置」处理（`failed / not_configured`，未发请求，可换键重试）。

### 4.2 配置

- `loadConfig()` → `loadConfig(provider)`；
- `activeProvider()`：返回 `Enabled && Configured` 的那个 provider，都没有则实时发码关闭；
- `UpdateConfig(provider, patch)`：启用一个 provider 时，**同一事务**停用另一个；
- `EnsureIssued` 的 `resolveCredentials` 改为按 provider 缓存。

### 4.3 发码调用

`callIssue` 只改一处：按 `request.Provider` 取网关与凭据。错误分类、`MaxAutoAttempts`、`count_mismatch`、占用租约全部沿用。

danew_cdk 响应到卡网状态的映射：

| danew_cdk 返回 | 网关返回 | 卡网请求状态 |
|---|---|---|
| 200 succeeded | `IssueResult` | `succeeded` |
| 4xx（含 422 `UPSTREAM_REJECTED`、403 额度/权限） | `GatewayError{Transient:false}` | `failed`，管理员可换新键重发 |
| 409 / 429 / 5xx（含 503 `UPSTREAM_PENDING`、502 `UPSTREAM_COUNT_MISMATCH`） | `GatewayError{Transient:true}` | 保持 `pending`，同键重试；8 次后 `unknown` |
| 网络错误 / 超时 | 同上 | 同上 |

### 4.4 `CheckOrderable`

- 用 `activeProvider()` 的 overview；没有启用的 provider → `ErrNotOrderable`；
- `RealtimeIssueSnapshot` 增加 `Provider`，由订单服务写进快照；
- danew_cdk 额外拦截：`issuable=false`；SKU 的套餐/地区不在 `client.allowed_plans` / `allowed_countries` 里（避免付款后才 403）；
- 低余额判断照旧用 `Balance.SpendableBalance`；
- 接口不可达时放行（与现状一致）。

### 4.5 退款停用

`DisableOrderCodes` 按请求的 `Provider` 分组调用各自网关。danew_cdk 的 `revoked` / `failed[].id` 映射到 `DisableResult.Disabled` / `Failed`，`failed[].reason`（如 `CONSUMED`）带进告警文案。

### 4.6 订单服务

`internal/modules/order/application/order_service_validate.go` 在 :207 调 `CheckOrderable`、在 :276 调 `ApplyItemSnapshot`，把 `realtimeIssue.Provider` 一起传入。fulfillment、refund、delivery_state 不感知 provider。

## 5. 新网关 `infrastructure/danewcdk`

| 方法 | 调用 | 说明 |
|---|---|---|
| `Issue` | `POST {base}/cdks/issue`，头 `Authorization: Bearer cn_live_...`、`Idempotency-Key`；body `{plan, count, payment_country, external_order_no, external_ref}` | 超时 75s（对方调卡台最长 60s） |
| `Overview` | `GET {base}/overview` | 填余额、套餐、地区、`Issuable`、`Client`；超时 15s |
| `DisableCDKs` | `POST {base}/cdks/revoke`，每批 ≤100 | 超时 30s |

细节：

- `IssuedCode.ID` 用对方返回的 `id`（danew_cdk 本地行 id），存进 `ProviderCDKIDs`，退款作废用；
- `external_order_no = request.OrderNo`，`external_ref = "item:{itemID}:chunk:{chunk}"`；
- 返回的 `request_id` 写入 `IssueResult.ProviderRequestID`，落到请求行；
- 响应体上限 4MB，错误信息截断 300 字，`User-Agent: dujiao-next-cdk-issue/1`；
- 错误体 `{"error_code","message"}` 解析进 `GatewayError.ErrorCode/Message`，后台直接可读。

## 6. 后台

### 6.1 设置页 `CdkIssueSettings.vue`

- 顶部改为 provider 切换：「danew_cdk（推荐）」/「ZovoCard 直连」；
- danew_cdk 表单：API 地址、API Key（只写不读，显示 hint）、默认付款地区、余额预警线、「测试连接」（展示主台状态、余额、可售套餐、本客户端额度）；
- ZovoCard 表单不变；
- 切换启用时确认弹窗：「切换后新订单走 X；已下单未发货的订单仍按下单时的渠道发码」。

### 6.2 发码记录 `CdkIssueRequests.vue`

- 增加「渠道」列与筛选；
- danew_cdk 请求显示 `provider_request_id`，可复制到 danew_cdk 后台搜索；
- 重试规则不变（`failed` → 新键，`unknown` / 卡住的 `pending` → 原键）。

### 6.3 商品绑定弹窗

套餐/地区下拉改用当前 provider 的 overview；danew_cdk 下只列本客户端被允许的套餐与地区。

### 6.4 商品兑换链接

切到 danew_cdk 后，买家应在 cdk.danew.cc 兑换（这样兑换记录、查单都在本站）。实时发码商品的 `redeem_url` 模板要指向 cdk.danew.cc，启用 danew_cdk 时在设置页提示检查。

## 7. 数据迁移

| 表 | 改动 |
|---|---|
| `cdk_issue_configs` | 加 `api_base_url varchar(255) not null default ''` |
| `cdk_issue_requests` | 加 `provider_request_id varchar(64) not null default ''` |
| 订单项快照 | 不迁移；缺 provider 视为 `zovocard` |

GORM AutoMigrate 加列即可。

## 8. 上线步骤

1. danew_cdk 上线卡网接口，后台新建客户端：允许套餐/地区与卡网在售商品一致，设置每日额度、IP 白名单（卡网服务器出口 IP），拿到 `cn_live_` Key；
2. 确认 OpenResty / Cloudflare 放行 `/api/v1/cardnet/*` 且不缓存；
3. 卡网上线本改造，ZovoCard 保持启用，在设置页填好 danew_cdk 并「测试连接」；
4. 检查实时发码商品的 `redeem_url` 指向 cdk.danew.cc；
5. 切换为 danew_cdk，用低价套餐走一遍：下单 → 发码交付 → cdk.danew.cc 查单与兑换 → 另一单退款作废；
6. 观察一段时间，ZovoCard 直连本期保留。

回滚：后台切回 ZovoCard；已按 danew_cdk 下单、未发码的订单仍走 danew_cdk。

## 9. 测试要点

- 历史快照（无 provider）的订单补发仍走 ZovoCard；
- 切换前后下的两笔订单各走各的 provider；
- danew_cdk 返回 503 后同键重试成功，只交付一次；
- danew_cdk 返回 422：请求 `failed`，管理员换新键重发成功；
- 退款作废：已兑换的码进入告警，带 `CONSUMED`；
- `CheckOrderable`：未授权套餐/地区、`issuable=false`、低余额时拦截；接口不可达时放行；
- 网关单测（httptest）覆盖 200 / 403 / 409 / 422 / 429 / 502 / 503 / 非 JSON / 超时。

## 10. 改动清单

| 文件 | 改动 |
|---|---|
| `internal/modules/cdkissue/domain/config.go` | `APIBaseURL`；按 provider 的 `BaseURL/Configured/DefaultConfig` |
| `internal/modules/cdkissue/domain/binding.go` | 快照读写带 provider |
| `internal/modules/cdkissue/domain/request.go` | `ProviderDanewCDK`、`ProviderRequestID` |
| `internal/modules/cdkissue/domain/overview.go` | `Issuable`、`Client` |
| `internal/modules/cdkissue/contract/ports.go` | `RealtimeIssueSnapshot.Provider`、`IssueResult.ProviderRequestID` |
| `internal/modules/cdkissue/application/*.go` | 多 gateway、`activeProvider`、按 provider 取凭据与停用 |
| `internal/modules/cdkissue/infrastructure/danewcdk/`（新） | 网关实现与测试 |
| `internal/bootstrap/cdkissue/wiring.go` | 注册两个 gateway |
| `internal/modules/order/application/order_service_validate.go` | 快照写入 provider |
| `frontend/admin/src/views/admin/CdkIssueSettings.vue`、`CdkIssueRequests.vue`、商品绑定弹窗、`api/admin.ts`、i18n | 见 §6 |
| `internal/modules/cdkissue/integrationtest/` | provider 切换、失败换键、退款作废用例 |
