# 需求包 A 技术细节：Session 多模态外存最小闭环

## 重点代码链路
- 用户消息：
    - `model.Message`
    - `model.ContentPart`
    - `AddImageData` / `AddAudioData` / `AddFileData`
    - `AddImageFilePath` / `AddAudioFilePath` / `AddFilePath`
- seed history 与消息改写：
    - `RunOptions.Messages`
    - `UserMessageRewriter`
- session 写入面：
    - runner 当前轮消息持久化
    - provider response / assistant event 持久化
    - server adapter 写入 `session.Events`
    - 直接 `session.Service.AppendEvent`
- 结构化多模态消息：
    - MCP image result 通过 `AddImageData` 形成 `ContentParts`
    - OpenClaw `MEDIA:` / `MEDIA_DIR:` 解析后形成图片消息

## 插入点判断
- 不宜在每个 DB backend 中实现：
    - DB backend 只应看到治理后的 payload，否则 mysql、redis、memory 等 backend 会重复实现相同逻辑。
- 不宜只在 runner 某个 append 调用点中实现：
    - AG-UI/A2A server adapter、team runtime、直接 session API 等路径可能绕开单个 runner 调用点。
- 候选方向：
    - session backend 之上的统一 persist helper。
    - session service decorator。
    - runner/session 之间的统一 persisted view 构造层。

## 设计注意点
- artifact service 如何传入治理层：
    - runner 配置。
    - invocation context。
    - session service decorator 初始化参数。
- session 信息如何参与 artifact 命名或元信息：
    - session ID。
    - app/user 信息。
    - event ID 或 message index。
- hydrate 默认策略：
    - 不建议 `GetSession` 默认回灌 bytes，否则容易抵消存储收益。
    - 倾向在模型请求构造处按需 hydrate，其余 consumer 显式请求。
