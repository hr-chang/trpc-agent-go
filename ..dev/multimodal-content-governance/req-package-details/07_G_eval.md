# 需求包 G 技术细节：Evaluation / EvalSet 治理

## 重点代码链路
- eval recorder 可能录制：
    - user content。
    - context messages。
    - intermediate responses。
    - final response。
- evalset backend：
    - local。
    - mysql。
- eval result / benchmark output 可能复制完整 payload。

## 设计注意点
- 录制线上多模态流量时尤其需要默认保护。
- eval replay 需要定义引用恢复方式。
- eval asset 可以选择：
    - artifact 副本。
    - 业务外部引用。
    - 摘要。
    - 受控 hydrate。
- 如果复用 telemetry/debuglog 的截断/摘要策略，需要保证不破坏评测语义。
