# GPT Image 1.0.2 交接说明（WIP 分支 `wip/1.0.2`）

> ⚠️ **未完整验证**：因额度不足，用户要求立即停止。本分支在最后一批改动（GIF 后端 / 前端 GIF 选项）之后**没有再跑 go vet / go test / tsc / vite build，也没有截图、没有真实出图测试**。合并前必须按下文“验证命令”全部跑一遍。
>
> 基线：远端 `main` @ `192dec0`（v1.0.1）。未打 tag，未合并 main。

## 各项需求状态

| # | 需求 | 状态 |
|---|------|------|
| 1 | 找回专用 Cloudflare 临时邮箱配置（多部署、按后缀路由） | ✅ 完成（后端+前端；当时 go vet / go test ./... 全绿；后续又改了别的文件，需复测） |
| 2 | 参数 popover 精简（删说明/提示框，压缩率仅 JPEG，紧凑 300px） | ✅ 完成（当时 tsc -b + vite build 通过；未截图） |
| 3 | 比例选择器放到工具栏，去掉“构图”概念 | ✅ 完成（同上，未截图） |
| 4 | GIF 输出 | 🟡 部分完成：后端主链路已写，**未编译未测试**；前端只加了 GIF 格式选项，**帧数/帧间隔输入框未做**；无单测；未做真实出图 |
| 5 | 创作台空状态压缩到 1280×760 不滚动 | ⛔ 未开始 |
| 6 | 顶部导航居中（3 列 grid） | ⛔ 未开始 |

---

## 1. 找回专用 Cloudflare 临时邮箱

**设计**
- `RecoverySettings.CloudflareMails []RecoveryCFMail`（json `cloudflare_mails`）：`id, name, base_url, admin_password, domains[], enabled`。
- 后缀规范化：小写、去 `@`、去首尾点、去重；支持 `*.example.com`（仅匹配子域，不匹配 example.com 本身）。精确匹配优先于通配；通配之间最长后缀优先。
- 保存校验（`validateRecoveryCFMails`）：同一后缀出现在两个条目 → 400；启用的条目没有 base_url → 400；base_url 必须 http(s)://。校验失败时**保留旧设置**。
- 密钥：`admin_password`（以及 `outlook_api_key`）落盘时 AES-GCM 加密（`sealRecoverySettings` / `openRecoverySettings`，复用 `secretbox.go` 的 EncryptSecret/DecryptSecret；旧的明文文档照常读取）。GET 返回 MaskSecret；POST 时按 `id` 用 UnmaskSecret 还原被回显的掩码。
- 路由顺序（`RouteMailChannel`）：账号手动指定 → 找回 CF 条目（按后缀）→ 注册机 CF 条目（旧行为，`reason="沿用注册机邮箱配置"`）→ Outlook 域名 → GPTMail。不跨渠道回退。手动指定 CF 时还会按账号 `mail_ref`（`recovery_cf:<id>`）找条目。
- `MailRoute` 新增 `entry_name`、`source`（`recovery` / `register`）。账号详情抽屉显示条目名称；“读取最新邮件”也返回 `entry_name`。
- 取信：找回 CF 条目先走 admin API `GET {base}/admin/mails?address=&limit=5&offset=0`（header `x-admin-auth`），失败/无密码时用账号 `mail_token` 走 `GET {base}/api/mails`（Bearer）。复用 mail_provider.go 里已有的 helper，**没有改 mail_provider.go**。
- 新接口 `POST /api/recovery/cloudflare-mails/test`，body `{id, base_url, admin_password}`（掩码密码按 id 还原），做一次 `/admin/mails?limit=1`，返回 `{ok, count}` 或 `{ok:false, error}`。
- 注意：`AccountRecovery.UpdateSettings` 签名改为返回 `(RecoverySettings, error)`。

**前端**：设置页“找回与自愈”里新增“Cloudflare 临时邮箱”子区：条目列表（名称 / Base URL / admin 密码（password 输入，显示掩码）/ 后缀 textarea（逗号或换行）/ 启用开关 / 测试连接 / 删除）+ 添加。Outlook 区标题改为“outlookEmail 渠道（Outlook 域名）”。

**文件**
- 新增 `internal/service/recovery_cf_mail.go`、`internal/service/recovery_cf_mail_test.go`、`internal/httpapi/recovery_cf_mask_test.go`
- 修改 `internal/service/account_recover.go`、`internal/service/recovery_mail.go`、`internal/httpapi/admin_ext.go`
- 修改 `web/src/lib/admin-api.ts`、`web/src/app/settings/components/recovery-settings-card.tsx`、`web/src/app/accounts/components/account-detail-drawer.tsx`

**兼容性**：没有 `cloudflare_mails` 的旧配置行为不变（仍走注册机 CF 配置，只是多了 reason 文案）。Outlook key 从明文变为加密存储，读取兼容明文。

**缺口**：未截图 `light-recovery-cf.png`。

## 2 & 3. 参数 popover 精简 + 工具栏比例

文件：`web/src/app/image/components/image-composer.tsx`
- popover 宽 300px、p-3、gap-2、行为“左标签 / 右控件”，控件 h-8 统一；只剩：张数（− 输入 + 步进器，1–10）、格式（Select）、压缩率（仅格式为 JPEG 时渲染）。
- 已删：顶部模型/链路说明块、所有蓝/粉/黄提示框、底部说明文字、构图/尺寸模式分段、比例/分辨率菜单、手动宽高、自定义比例、`ImageSizePreviewPanel` 组件、`sizePreviewDetail`。
- 工具栏“参数”按钮左侧新增“比例”按钮（h-9，与其它工具栏控件一致），点开即选：Auto、1:1、3:2、2:3、16:9、21:9、4:3、3:4、9:16。选 Auto → `sizeMode="auto"` + `aspectRatio=""`（即原“不写入固定比例”）；选比例 → `sizeMode="ratio"` + 对应比例。
- 缺口：Codex 链路原有的“分辨率（1080P/2K/4K）”和“手动宽高”UI 已随构图一起移除（状态和后端逻辑保留，localStorage 里的旧值仍会生效；若旧值是 custom，工具栏显示“自定义”，选任一比例即可覆盖）。如需给 Codex 保留分辨率，需另加。未截图 `light-image-params.png` / `light-image-ratio.png`。

## 4. GIF 输出（部分）

已做（后端，未编译）：
- `internal/service/image_gif.go`（新）：`IsGIFBytes`（GIF87a/GIF89a 魔数）、`SniffImageFormat`、`GIFFrameCountOf`（遍历 GIF 块数帧）、`NormalizeGIFFrameOptions`（帧数 2–24 默认 8；帧间隔 20–2000ms 默认 120）、`GIFFrameOptionsFromBody`、`AppendGIFPromptInstruction`（“输出为动画 GIF，共 N 帧，每帧 M 毫秒…”）、`ValidateGIFStreamRequest`。
- `internal/service/image_task.go`：`NormalizeImageOutputFormat` 接受 `gif`；`ImageOutputOptions` 加 `FrameCount/FrameDurationMS`；`mergeImageOutputOptions` 写入 `frame_count/frame_duration_ms`。
- `internal/httpapi/routes.go`（任务 JSON 入口读帧参数）、`internal/httpapi/app.go`（multipart 编辑表单读 `frame_count/frame_duration_ms`）。
- `internal/protocol/conversation.go`：`ConversationRequest` / `ImageOutputOptions` 加帧字段；`Normalized()` 对 gif 做 clamp；`StreamResponsesImageOutputs` 中 gif 时只在**发往上游的 prompt**追加指令（不改存储的用户 prompt），上游 `output_format` 改发 `png`，模型 slug 不变；`formatImageResultWithOptions`：结果字节是 GIF → 不转码，存 `.gif`；请求 gif 但上游返回静态图 → 原样保存并按真实格式（sniff）标记 `output_format`。
- `internal/protocol/api.go`：generations / edits / chat-image 流式均在 `gif + stream:true` 时返回 400；chat-image 通过 `ImageOutputOptionsFromPayload` 取帧参数。

未做 / 待确认：
- **前端帧数 / 帧间隔(ms) 输入框**（popover 中 GIF 时显示，隐藏压缩率——压缩率已经只在 JPEG 显示）；需要在 `image-composer.tsx` 加 props、`page.tsx` 加状态+localStorage、`web/src/lib/api.ts` 的 `createImageGenerationTask` / `createImageEditTask` 透传 `frame_count` / `frame_duration_ms`。目前前端已加 `gif` 格式选项（`web/src/lib/api.ts`），选 GIF 时后端用默认 8 帧 / 120ms。
- 确认 `/images/*.gif` 的静态服务 Content-Type 为 `image/gif`（按扩展名应自动正确，未验证）。
- 确认 `service/image.go` 元数据 / 图库对 `gif` 格式的展示；结果卡是否正确显示“GIF”。
- `/v1/responses` image_generation 工具路径（`api.go` 约 1201 行）未加帧参数与 stream 校验。
- 单测未写：建议覆盖 `IsGIFBytes`、`SniffImageFormat`、`GIFFrameCountOf`（用 `image/gif` 编一个 3 帧 GIF）、`NormalizeGIFFrameOptions`、`ValidateGIFStreamRequest`、`formatImageResultWithOptions` 的 GIF 旁路与静态图回退、`Normalized()` 非 gif 清零帧参数。
- 真实出图测试（只允许 1 次调用）未做。

## 5. 创作台空状态（未开始）
目标：1280×760 视口无需滚动看到 header → 灵感预设卡片标题/描述/meta。要点：顶部 padding 减半；标题 ~32px；STUDIO 眉标+一行副标题，间距 8px；composer textarea min-height ~88px（现 ~128）；composer→灵感预设 间距 ~24px（现 ~60）；预设标题区紧凑；卡片图高度减小（3:2 放不下就 16:10）。保持左边缘对齐与 36px 控件。截图 `light-studio-760.png`（视口截图，非整页）。相关文件：`web/src/app/image/page.tsx`、`web/src/app/image/components/image-composer.tsx`（placement="inline"）。

## 6. 顶部导航居中（未开始）
lg（≥1024）以上 header 改为 `grid grid-cols-[1fr_auto_1fr]`：logo 左、导航链接居中、深色/退出右（`justify-self-end`）；保留激活下划线、高度、暗色；<1024 保持现有左流式/横向滚动。文件在布局外壳组件（`web/src/components/` 下的 top nav / app shell，未定位具体文件）。不要动 `web/src/app/register`。

---

## 验证命令（Instabox 测试机 /data/home/gptchat2api-cf）

```bash
export PATH=/data/home/go1262/go/bin:$PATH GOTOOLCHAIN=local   # Go 1.26.2
go vet ./...
go test ./... -count=1
gofmt -l internal/service/recovery_cf_mail.go internal/service/recovery_cf_mail_test.go internal/service/image_gif.go internal/protocol internal/httpapi
cd web && npx tsc -b && npx vite build
```
本地脚本：`work/bin/sync.sh`（同步源码到测试机）、`work/bin/gt.sh "<cmd>"`（在测试机仓库目录执行）、截图 `work/shot.js` + `work/bin/pull.sh`；截完 `pkill -f '^/tmp/g2a'; rm -rf /tmp/shot`。

## 硬性约束
- 不要修改：`internal/service/register*.go`、`register_flow.go`、`register_sentinel_node.go`、`flaresolverr.go`、`mail_provider.go` 及它们的测试；`/api/register` 路由；`web/src/app/register`。
- 不要碰 FreeQwenApi、qwen-web-test、现有 Chromium；不要动用户 VPS。
- 推送用 GitHub git data API（HTTPS git push 不可用），shell credentials `[{"provider":"github-sakuralaaa"}]`；仓库 `Sakuralaaa/GPT-Image`（私有）。
- 不要打印/提交任何密钥。合并 main / 打 tag 前需用户确认。
