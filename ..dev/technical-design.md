# SWE-Bench Verified 技术方案索引

本目录承载 SWE-Bench Verified 评测的技术方案。需求边界见
[requirements.md](requirements.md)，技术实现按“公共底座 + 两条主线”拆分。

## 文档结构

- [技术方案总览](technical-design/README.md)
- [公共底座方案](technical-design/shared-foundation.md)
- [Baseline 复现方案](technical-design/baseline-reproduction.md)
- [Native 实现与优化方案](technical-design/native-implementation.md)

## 推荐评审顺序

1. 先看 [技术方案总览](technical-design/README.md)，确认整体拆分和交付边界；
2. 再看 [公共底座方案](technical-design/shared-foundation.md)，确认 dataset、verifier、archive、report 和状态口径；
3. 然后看 [Baseline 复现方案](technical-design/baseline-reproduction.md)，确认参照系是否可信；
4. 最后看 [Native 实现与优化方案](technical-design/native-implementation.md)，确认 Go-native SWE Agent 的实现和优化路径。

## 核心拆分

```text
公共底座：
  dataset / doctor / official local harness / archive / report

主线 A：baseline 复现
  mini-SWE-agent -> predictions -> local harness -> baseline results

主线 B：native 实现与优化
  tRPC-Agent-Go native agent -> predictions -> local harness -> native results

最终合流：
  baseline results + native results -> comparison -> bilingual report
```
