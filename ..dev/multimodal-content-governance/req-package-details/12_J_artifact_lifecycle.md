# 需求包 J 技术细节：Artifact 生命周期管理

## 技术动机
- A 包只负责单次 append 的正确性：artifact save 成功但 append 失败时提交 best-effort delete。
- 这不能覆盖长期运行后的 orphan artifact、session 删除后的联动清理、历史迁移后的引用关系校验。
- C/D 等包继续扩大 artifact/workspace ref 的生产面后，缺少生命周期治理会让存储增长不可控。

## 当前实现基线
- A 包 `ContentRef` 已记录：
    - `artifact_ref`。
    - `artifact_name`。
    - `artifact_version`。
    - `mime_type`。
    - `size_bytes`。
    - `sha256`。
    - `original_name`。
- A 包保存 artifact 时已有 session owner 信息：
    - app。
    - user。
    - session。
- A 包没有：
    - 全局反引用索引。
    - session 删除联动 artifact 删除。
    - orphan 扫描。
    - TTL / retention 策略。

## 重点链路
- session 删除：
    - `DeleteSession` / 批量删除 / 用户数据清理。
- artifact 查询：
    - backend 是否支持 list、metadata filter、version delete。
- 引用扫描：
    - session event `ContentRef`。
    - tool output ref。
    - workspace output ref。
    - eval/debug/replay 保存的 ref。

## 设计注意点
- 第一版可优先做安全 dry-run：
    - 扫描 session refs。
    - 扫描 artifact refs。
    - 输出 orphan candidate 和仍被引用的 artifact。
- 删除策略要保守：
    - 默认不误删 pinned refs。
    - 删除失败不能破坏 session 读取。
    - 支持重试和人工确认。
- 反引用索引是否必要取决于 artifact backend 能力和规模；可先设计接口空间，首版用扫描实现。
- provider file id 生命周期不应混入本包，provider cache 属于需求包 I。

## 测试关注点
- 同一 artifact 被多个 ref 使用时不会误删。
- 删除 session 后可列出或清理仅属于该 session 的 artifacts。
- orphan dry-run 输出稳定、可解释。
- 清理失败保留原始 session/ref 语义，并能报告失败对象。
