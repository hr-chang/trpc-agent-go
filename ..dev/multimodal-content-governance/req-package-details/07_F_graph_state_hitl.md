# 需求包 F 技术细节：Graph Checkpoint / State / HITL Payload 泄漏守护

## 已确认真实风险
- graph checkpoint 可能保存 messages/state snapshot。
- checkpoint 的多模态可能来自：
    - session history 回灌 messages。
    - 当前轮 messages。
    - one-shot messages。
    - agent-input messages。
    - graph state 中携带的 message/tool output/file content。

## HITL / Graph payload 风险
- Interrupt / Resume payload 可能保存用户确认上下文、工具中间结果、文件引用或大对象。
- graph loop / dynamic dispatch state 可能积累多轮中间结果。
- 多路并行 join 前后的 state 可能携带多个分支的检索结果、tool result 或文件产物。
- 子图 completion relay 可能通过 final state 透传 `StateDelta` 到父 session。

## agent-as-tool relay 风险
- 需要关注：
    - `FinalResultStateChunk`
    - 子图 completion `StateDelta`
    - 父 session tool result event
- 重点防止：
    - `messages`
    - `user_input`
    - `last_response`
    - tool output / file content
    - 未来新增类似大对象 key

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
    - 防止 graph checkpoint、HITL payload 和子图 relay 泄漏多模态 state。
    - 守护框架内部未来不要写入 raw bytes/base64。
    - 对业务自定义 state 做文档约束或可选策略。

## 设计注意点
- checkpoint 恢复语义不能被破坏。
- 哪些 state 必须完整恢复，哪些可以保存 ref/summary，需要按 graph/HITL 场景确认。
- F 不应替业务全量扫描所有 state；应优先治理框架内部确定路径和已知高风险 key。
