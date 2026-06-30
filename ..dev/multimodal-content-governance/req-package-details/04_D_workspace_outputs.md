# 需求包 D 技术细节：Workspace / Sandbox / Skill 文件产物治理

## 重点代码链路
- conversation file 可能来自：
    - `host://` path
    - `artifact://` ref
    - provider file ID
    - inline `File.Data`
- 字节物化路径：
    - `workspaceinput.ResolveFileBytes`
    - `StageConversationFiles`
- 执行产物路径：
    - local / remote CodeExecutor 输出文件
    - sandbox workspace 文件
    - skill 输入/输出文件
    - OpenClaw uploads
    - per-session workspace

## 设计注意点
- dereference 后的 workspace bytes 不应因为“已经被下载成文件”而再次进入 session/state 的长期存储本体。
- 输出文件应优先通过 artifact/workspace ref 表达，而不是把文件内容塞回 tool result 或 state。
- 需要区分：
    - 临时 workspace 文件生命周期。
    - artifact 生命周期。
    - 应用侧 uploads 生命周期。
    - remote CodeExecutor workspace 生命周期。
- workspace ref 与 artifact ref 不是同一层能力：
    - workspace ref 更适合运行期文件定位。
    - artifact ref 更适合跨轮、回放、长期引用。

## 与其他包的关系
- C 管 tool result / execution output 的外部表达。
- D 管文件物化、产物位置和生命周期。
- I 可复用 D 的 artifact/workspace ref 作为 provider 附件上传输入。
