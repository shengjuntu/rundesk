# RunDesk 0.16.0 — Setup and a conversation-first interface

[English README](../README.md) · [中文说明](../README.zh-CN.md)

## Changes / 本版变化

- Separate narrow global navigation and resizable conversation history. At 1366×768, history has more than 380 pixels of usable vertical space. Mobile uses a history drawer.
- English/Chinese switching, remembered in the browser. Static interface labels use a reviewed catalog; user content and original tool logs are preserved. Legacy detailed messages may still use their original language.
- Environment setup works without an installed Codex executable. Automatically checks executable, workspace and Docker; native model/account/MCP probes are explicit. No probe is described as a completed inference test.
- Save a Codex executable, default model and selected workspace. Completed setup does not reset on subsequent saves. Existing conversations retain their model. Busy processes remain active; idle native connections reload.
- Configure a Responses API-compatible provider through native Codex settings. Secrets are referenced by environment variable name. Provider configuration is applied in several native writes; an error may leave partial settings, so review configuration and retry before running tasks.
- Applications show task history before collapsed configuration. Legacy integration instructions point to `/connect`.
- Create a Docker build draft directly from the embedded template. Building an application image no longer requires first switching that application to Docker. Starting a build and applying its image remain explicit operations.

## Scope / 范围

初始化页不是操作系统安装器。缺少 Codex 时提供安装命令，不自动提权安装 Docker。远程管理令牌仍通过 RUNDESK_TOKEN 配置；数据目录由启动参数决定，不迁移在线数据库。模型的密钥由服务进程环境提供。

自动接入应用仍默认使用本机 Codex；没有新增注册时自动创建 Docker 容器的流程。应用镜像构建、选择及部署仍由管理员执行。Docker 执行继续要求 Linux 与本机 Docker Engine。

初始化和环境检查沿用现有 API 管理员权限与来源校验。应用凭据与普通成员不能读取或修改系统初始化配置。没有新增匿名安装入口。

## APIs

All routes require administrator access and are available under `/api/v1`:

| Route | Purpose |
|---|---|
| GET /setup | Current setup state, runtime paths and service account |
| PUT /setup | Revision-checked configuration save |
| POST /setup/check | One bounded check: codex/workspace/models/account/mcp/docker |
| POST /setup/provider | Configure the general assistant's custom model provider |
| GET /setup/docker-template?version=X.Y.Z | ZIP context with a pinned Codex release |

A check returns `ok`, `kind`, `result`, `error`, `checkedAt`, and `demo`. `ok` means the individual probe completed; account/MCP payloads still need review. This endpoint does not run a model inference. Docker probes can fail independently without blocking local chat setup.

## Upgrade

Back up data and Codex configuration, stop the old process, replace the binary, and keep the same data location. Frontend files and templates are embedded; copying source files alone does not update an existing binary. No changes to news2douyin or the video application are required for this UI release.

## Validation

Go package tests and browser checks cover missing-Codex setup, revision conflicts, administrator isolation, template version validation, history sizing, language persistence, original application names, build-draft creation, and desktop/mobile rendering. Browser checks use the demo protocol simulator. No real Docker engine, remote model provider or production deployment was validated in this workspace.
