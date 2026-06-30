# 需求包 B 技术细节：AG-UI / Client Replay 多模态治理

## 重点代码链路
- AG-UI 输入：
    - `InputContent.Data`
    - `InputContent` 中的 URL 或 data URL
- AG-UI 存储路径：
    - `session.Tracks`
    - AG-UI custom event payload
    - MessagesSnapshot / 前端 replay
- 客户端回放面：
    - event bridge 转前端协议
    - SSE translator
    - 业务侧 replay/cache/debug payload

## 设计注意点
- AG-UI 输入通常可能同时进入：
    - session event。
    - track event。
- 需求包 A 只能覆盖 session event 主路径，不能自动覆盖 track 独立 payload。
- Client replay 面是否纳入治理，关键看它是否承担存储、缓存、回放或 debug 留存职责。
- 前端 replay 需要决定返回：
    - artifact ref。
    - 外部 URL。
    - 摘要。
    - hydrate 后内容。
    - 或不可恢复时的可解释提示。

## 边界提醒
- 纯透传 SSE translator 不应被强行纳入存储治理。
- 如果 translator 同时写 debug/replay/cache，则应按本包或 E1 约束处理。
