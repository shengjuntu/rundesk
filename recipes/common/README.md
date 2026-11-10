# 共用的配方输入 / Shared recipe inputs

这里保存多份配方使用的普通文件：安装工具、运行检查、媒体桥接，以及已审阅并修复的 Agent-Reach 本地源码。文件仍可直接编辑，不需要重新编译 RunDesk。通用 Docker 模板保留自己的工具，可继续单独构建。`bases/` 还保存 system、media、browser 三层可编辑 Dockerfile、对应的计划和共用安装脚本；这些文件会一同导出到上下文的 `bases/`，供服务端和 CLI 自动准备基础镜像。

每个配方的 `recipe.json` 用顶层 `sharedFiles` 声明“导出目标路径 → 此目录下的来源路径”，例如：

```json
{
  "sharedFiles": {
    "tools/install-codex.sh": "tools/install-codex.sh",
    "vendor/agent-reach": "vendor/agent-reach-cf70302e"
  }
}
```

来源可以是普通文件或目录。构建和导出时，RunDesk 与 Python CLI 都将选定内容复制为完整上下文中的普通文件。导出的 ZIP 不依赖 `common/` 或原机器路径；`profile.lock.json` 不保留 `sharedFiles`。历史构建保存冻结后的内容，之后编辑共用文件只影响新构建。

若某个配方需要单独修改工具，可在配方目录内创建同名普通文件，例如 `news2douyin/tools/install-codex.sh`；它会覆盖共用版本。共用来源仍必须存在且有效。文件与目录之间的冲突、重叠映射、路径越界、符号链接和特殊文件会被拒绝。只修改一个配方时优先用本地覆盖，避免影响其他配方。

Agent-Reach 的完整文件清单、摘要、修复补丁和许可证继续保留。不要直接裁剪其中的文件，也不要换回构建时从 GitHub 下载。变更审阅版本时必须一起更新源码、清单、摘要与依赖声明，并确认默认和轻量变体均可导出。

The `bases/` inputs define reusable system, media and browser image layers. Managed builds prepare these layers before the preset and use immutable parent image IDs. Exported contexts retain a standalone installer fallback for ordinary Docker builds. Codex and business dependencies stay in each preset. A new standalone CLI invocation can recreate a pruned cache image from its pinned inputs; this does not alter server-side historical build identities.

This directory holds editable inputs shared by several recipes. Each recipe's top-level `sharedFiles` maps a destination inside its exported context to a file or tree here. Both RunDesk and the Python CLI copy these inputs into ordinary, self-contained build files; frozen metadata does not retain authoring references.

A regular file at the same destination inside a recipe explicitly overrides its shared version. The declared shared source must still exist and pass validation. Use a local override for changes intended for one recipe. Paths cannot escape the recipe root, mappings cannot overlap, and links, special files, file/directory conflicts and generated metadata destinations are rejected.

Keep the complete reviewed Agent-Reach source, LICENSE, patch and digest manifest together. Its installation stays local to the build context. Updating a shared input affects future snapshots; existing build snapshots remain unchanged.
