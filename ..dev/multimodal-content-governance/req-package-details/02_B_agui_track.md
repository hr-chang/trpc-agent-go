# 需求包 B 技术细节：AG-UI Track 多模态治理

## 重点代码链路
- AG-UI 输入：
    - `InputContent.Data`
    - `InputContent` 中的 URL 或 data URL
- 存储路径：
    - `session.Tracks`
    - AG-UI custom event payload
    - MessagesSnapshot / 前端 replay

## 设计注意点
- AG-UI 输入通常可能同时进入：
    - session event。
    - track event。
- 需求包 A 只能覆盖 session event 主路径，不能自动覆盖 track 独立 payload。
- 前端 replay 需要决定返回：
    - artifact ref。
    - 外部 URL。
    - 摘要。
    - hydrate 后内容。
    - 或不可恢复时的可解释提示。
