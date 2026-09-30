# device-s7 上游同步评估（2026-09-30）

建议安排合并上游 main，以同步正式 SDK、依赖和构建工具；这不是修复 S7 读写或提高吞吐量的紧急更新。合并必须保留本地驱动修复、kenvaid/gos7 最新版本及用户的 profile 修改。隔离合并副本已通过测试、vet、build，但尚未在正式仓库执行合并或推送。

## 比较范围

- 官方 main：`1412a1d2932a6cdb02bc61bd3799417dd26df4b5`，最新提交日期 2026-05-26。
- 本地 main：`5c52914f344c`；公共祖先：`2baafd2a07c4e9f110a91abc444397b8386850e0`。
- 本地独有 2 个已提交变更；上游独有 23 个提交，其中 14 个非合并提交、9 个合并提交。上游净修改 9 个文件。
- 另将当前五个已跟踪文件的未提交修改复制到临时副本，检查实际待合并状态，包含用户 profile 修改。
- 来源为实时 Git 获取的官方仓库和下载的 SDK 模块源码：[上游完整对比](https://github.com/edgexfoundry/device-s7/compare/2baafd2a07c4e9f110a91abc444397b8386850e0...1412a1d2932a6cdb02bc61bd3799417dd26df4b5)。

## 主要变化及处理建议

| 范围 | 上游最终变化 | 建议 |
| --- | --- | --- |
| EdgeX SDK | `v4.1.0-dev.38` 经多次开发版更新后切换为正式版 `v4.0.2` | 建议同步；这是开发版本到正式版本的切换，不能只按版本数字判断新旧 |
| Core contracts / 基础模块 | contracts `v4.0.3`、bootstrap `v4.0.5`、configuration/messaging `v4.0.3`、registry/secrets `v4.0.2` | 与 SDK 成套采用，重新 tidy；仍需验证实际部署的 EdgeX 服务 |
| Go / Docker / CI | Go `1.23.7` → `1.25.0`；构建镜像 `golang:1.25-alpine3.22`，运行镜像 Alpine `3.22`，Jenkins Go `1.25` | 建议一起采用，避免只改 go.mod 导致旧镜像无法按预期构建 |
| Lint | golangci-lint `1.61.0` → `2.5.0`；配置迁移至 v2 格式 | 与 Makefile 安装版本同步，不能只复制配置 |
| 间接依赖 | 更新 crypto/net、gRPC、Echo、NATS、OpenZiti、OpenAPI 等；增加新的许可归属 | 建议随 SDK 同步；不将这些版本变化笼统称为已验证的安全修复 |
| S7 驱动 | 唯一 Go 源码变化为 `strings.Replace(..., -1)` → `strings.ReplaceAll(...)` | 当前本地新解析实现已有 ReplaceAll；保留本地驱动 |
| gos7 | 官方使用 `robinson/gos7@7ea1d6fb9d20` | 保留 `kenvaid/gos7@0a8e4f3e873c`；不能用上游 go.mod 覆盖此行 |
| 文档 | 增加 4.0.2 changelog、Attribution 依赖许可条目 | 建议同步；changelog 中的构建提交链接误指向 device-onvif-camera，应纠正 |

上游没有修改批量读写调度、连接生命周期、错误传播、PDU 分包、资源映射、profile 样例或驱动测试。因此不能通过此次合并解决原 device-s7 审查中剩余的问题，也没有新增 PLC 并发能力。

## SDK 收益的核对

核对了 SDK `v4.1.0-dev.38` 和 `v4.0.2` 的实际源码，避免将发布说明中的历史变化全部当成本次新增收益。

- 两个版本的 `internal/cache/devices.go`、`profiles.go`、`provisionwatcher.go` 完全相同；缓存并发保护已存在于当前版本。
- 两个版本的 AutoEvent executor、配置结构完全相同，当前版本已具备 `SendChangedReadingsOnly`。
- 当前版本已经支持 ObjectArray，以及读取时的 mask/shift；相关 transformer 入口和参数转换文件相同。
- 正式版新增可选扩展接口 `DeviceServiceSDKExt`，提供 Add/Update/PatchDeviceWithoutValidation；原 DeviceServiceSDK 基础接口保留。当前驱动未使用这些新增接口。
- 主要实际收益为采用官方正式 SDK、更新间接依赖和工具链；未测得吞吐量变化。

SDK 发布依据：[v4.0.2 changelog](https://github.com/edgexfoundry/device-sdk-go/blob/v4.0.2/CHANGELOG.md)、[SDK 接口](https://github.com/edgexfoundry/device-sdk-go/blob/v4.0.2/pkg/interfaces/service.go)。

## 合并冲突与验证

`git merge-tree --write-tree HEAD upstream/main` 的已提交历史预演仅冲突 go.mod/go.sum。复制当前未提交修改后，在临时仓库执行实际合并，冲突扩大为三个文件：

| 文件 | 处理方案 |
| --- | --- |
| go.mod | 采用上游 Go/SDK/contracts/依赖版本，保留 kenvaid/gos7 最新 require，保持无本地 replace |
| go.sum | 基于选定的 go.mod 执行 go mod tidy，重新生成校验记录 |
| internal/driver/driver.go | 保留当前修复实现，其中已包含上游 ReplaceAll 变化；禁止整文件采用上游版本 |

其他文件自动合并，包括保留本地 Makefile 的 arm64 目标及 `-s -w` 参数。临时副本的用户 profile 和 driver_test 修改也保留。

临时目录：`C:/Users/kenvaid/AppData/Local/Temp/device-s7-upstream-review-e2ae17dbc4474ae5a372251c7c316568`。其中包含仅用于预演的临时快照提交，未关联正式分支。

在该临时副本选定上述合并内容后，使用本机 Go 1.26.4 执行：

```text
go mod tidy
go list -m github.com/edgexfoundry/device-sdk-go/v4 github.com/edgexfoundry/go-mod-core-contracts/v4 github.com/kenvaid/gos7
go test -count=1 -timeout=90s ./...
go vet ./...
go build ./...
```

均以退出码 0 完成；实际解析的三个模块为 SDK `v4.0.2`、contracts `v4.0.3`、gos7 `v0.0.0-20260930084703-0a8e4f3e873c`。临时副本 driver.go 与正式工作区当前文件逐字节一致。

本次兼容性验证只包含已有驱动单元测试、编译和静态检查；没有复制未跟踪的 doc 模拟测试到临时副本，没有执行真实 PLC / 模拟 PLC、race、Docker 构建、golangci-lint v2 或完整 EdgeX 服务联调。因此可以确认当前 API 和构建基本兼容，不能据此保证全部运行行为。

## 建议实施顺序

1. 保存当前本地驱动修复、依赖更新和测试，单独保留用户 profile 修改，避免直接在脏工作区合并。
2. 在同步分支合并上游 main 的完整历史，按上述方案处理三个文件；无需逐个 cherry-pick 已被后续版本替代的依赖升级。
3. 一起更新 Go、Docker、CI 和 lint 配置，tidy 后检查 Attribution，完成驱动回归、模拟器、race、Docker 和所用 EdgeX 环境的验证。
4. 验证通过后合并至 kenvaid/device-s7 的 main 并推送。

正式工作区当前分支和源文件未因本次评估改变；仅获取 upstream/main 跟踪引用并新增评估报告。
