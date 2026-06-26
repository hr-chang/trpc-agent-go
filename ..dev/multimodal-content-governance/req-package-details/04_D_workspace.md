# 需求包 D 技术细节：Workspace Dereference 与临时文件治理

## 重点代码链路
- conversation file 可能来自：
    - `host://` path
    - `artifact://` ref
    - provider file ID
    - inline `File.Data`
- 字节物化路径：
    - `workspaceinput.ResolveFileBytes`
    - `StageConversationFiles`
- 产物路径：
    - codeexecutor 输入/输出文件
    - skill 输入/输出文件
    - OpenClaw uploads

## 设计注意点
- dereference 后的 workspace bytes 不应因为“已经被下载成文件”而再次进入 session/state 的长期存储本体。
- 输出文件应优先通过 artifact/workspace ref 表达，而不是把文件内容塞回 tool result 或 state。
- 需要区分：
    - 临时 workspace 文件生命周期。
    - artifact 生命周期。
    - 应用侧 uploads 生命周期。
