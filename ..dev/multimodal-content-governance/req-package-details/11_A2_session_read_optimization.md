# 需求包 A.2 技术细节：Session 外存读取优化

## 技术动机
- A 包当前为了兼容业务，`GetSession`、完整 `ListSessions`、`SearchEvents`、`GetEventWindow` 默认 hydrate。
- 默认 hydrate 保证历史行为平滑，但也意味着读取 session 仍可能触发大量 artifact load。
- A 包已经把 persisted view 和 runtime view 分离，A.2 应在不破坏默认兼容性的前提下，补充显式 persisted view / without-hydrate / lazy hydrate 能力。

## 当前实现基线
- 公开入口：
    - `session/externalization.Wrap(inner, artifactService, externalization.Config{Enabled:true})`。
- 写入：
    - `AppendEvent` 在进入 backend 前构造 persisted event。
    - 被治理的 `ContentParts` 写入 `ContentRef`，清空 inline bytes/data URL。
- 读取：
    - `GetSession` 默认 hydrate。
    - `ListSessions` 对 `WithListSessionOnlyMeta` 跳过 hydrate。
    - `SearchEvents` / `GetEventWindow` 在 wrapper 保留可选接口时默认 hydrate。

## 重点链路
- session read APIs：
    - `GetSession`。
    - `ListSessions`。
    - `SearchEvents`。
    - `GetEventWindow`。
- model request 构造：
    - 正常路径依赖默认 hydrate。
    - A.2 后需要防止 persisted view / injected message 中的 unresolved `ContentRef` 被外发。
- replay / debug / eval：
    - 默认可读取摘要或 ref。
    - 需要完整内容时显式 hydrate。

## 设计注意点
- 默认行为保持 hydrate，新增 option 必须显式 opt-in。
- without-hydrate 返回值应保留足够元信息：
    - `artifact_ref`。
    - content type。
    - mime / original name / size / sha256。
    - data URL 来源标记。
- unresolved ref guard 应位于 provider request 构造前，而不是 provider adapter 内部承担 session storage 语义。
- lazy hydrate 粒度不宜过早承诺复杂 API；可以先提供 event/message/content-part helper 或内部 consumer 入口。

## 测试关注点
- 显式 without-hydrate 不触发 artifact load。
- 默认读取保持 hydrate，历史 inline / ref / mixed session 行为不变。
- provider 前 unresolved `ContentRef` 明确报错，不进入 URL / FileID / data 字段。
- artifact 缺失、hash/size 校验失败、invalid ref 都保留明确错误语义。
