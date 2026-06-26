# 需求包 F 技术细节：Checkpoint 与 State 泄漏守护

## 已确认真实风险
- graph checkpoint 可能保存 messages/state snapshot。
- checkpoint 的多模态可能来自：
    - session history 回灌 messages。
    - 当前轮 messages。
    - one-shot messages。
    - agent-input messages。
    - graph state 中携带的 message/tool output/file content。

## agent-as-tool relay 风险
- 子图 completion relay 可能通过 final state 透传 `StateDelta` 到父 session。
- 需要关注：
    - `FinalResultStateChunk`
    - 子图 completion `StateDelta`
    - 父 session tool result event

## 既有过滤行为
- graph completion 写入 session 前已有剥离大对象倾向的过滤逻辑。
- 需要重点守护的语义：
    - 剥离 `messages`
    - 剥离 `user_input`
    - 剥离 `last_response`
    - 防止后续新增类似大对象 key

## StateMap / StateDelta 事实判断
- `StateMap` 是 `map[string][]byte`，可以承载 JSON，也可以承载任意 bytes。
- 框架内部现状主要写入：
    - 控制位。
    - 路由信息。
    - artifact ref。
    - skill 标记。
    - 小 JSON。
- 因此“治理所有框架内部 StateMap 写入的大量多模态对象”不是当前真实闭环需求。
- 真实需求是：
    - 防止 graph checkpoint 和子图 relay 泄漏多模态 state。
    - 守护框架内部未来不要写入 raw bytes/base64。
    - 对业务自定义 state 做文档约束或可选策略。
