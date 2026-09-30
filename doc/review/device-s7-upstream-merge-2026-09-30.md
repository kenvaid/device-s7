# device-s7 上游合并记录（2026-09-30）

本次合并保留官方 upstream/main 的完整历史、kenvaid/device-s7 的远端 M 区修复历史，以及当前本地驱动修复和测试。同步分支为 `codex/sync-device-s7-upstream-20260930`，验证后快进合入本地 main；本轮不推送远端。

## 合并内容

- 官方上游：`1412a1d2932a6cdb02bc61bd3799417dd26df4b5`，23 个上游独有提交。
- 自有远端：`58a8e16`，修复读 M 区失败；获取 origin 后发现此前本地 main 落后一条。当前新解析器已覆盖其效果，保留该提交历史。
- 本地修复先保存为 `e50ece3`：gos7 依赖更新、字节/位地址解析、边界检查、单元测试、模拟器及评审记录。用户 profile 修改排除在提交之外。
- 先合入 origin/main，再合入 upstream/main；go.mod/go.sum/driver.go 冲突已按评估方案解决。
- SDK 为 `v4.0.2`，contracts 为 `v4.0.3`，Go 声明为 `1.25.0`。
- 保留 `kenvaid/gos7 v0.0.0-20260930084703-0a8e4f3e873c`，没有本地 replace，也没有改回 robinson/gos7。
- 保留本地 Makefile 的 arm64 目标及 `-s -w` 参数，同步 Go/Alpine/Docker/Jenkins/lint 配置。
- 修正上游 changelog 错误的项目链接；补齐 xxhash/v2、ulid/v2 许可归属，并将 gos7 归属改为实际使用的 kenvaid 仓库及 BSD-3 许可。
- CT 模拟测试的数据生成改为 uint16 运算，消除新版 gosec 的转换告警；数据模式和测试范围保持一致。

## 正式工作区验证

| 检查 | 结果 |
| --- | --- |
| Go 1.25.1：test / vet / build | 全部通过，driver 语句覆盖率 14.4% |
| Go 1.26.4 / WSL：race | 全部通过 |
| golangci-lint 2.5.0 + Go 1.25.1 | 0 issues |
| Python 补充模式 typed，三轮 | 固定报文检查及 M/I/Q/CT、混合类型、边界错误、共享/独立连接测试全部通过；并发读写 4800 次请求 |
| go.mod 依赖归属检查 | 所有直接及间接 require 均有 Attribution 条目 |
| 用户 profile SHA256 | 与合并前备份一致，保持未提交 |
| Docker 完整构建 | 未完成：Alpine GCC 包下载阶段约六分钟无新进展，随后主动取消本任务的构建；未生成最终镜像 |
| Windows ServerDemo | 当前本机 102 端口没有运行实例，本轮未重跑连接测试 |
| EdgeX 完整服务联调 | 未执行 |

WSL 首次 race 依赖下载因 proxy.golang.org 连接超时失败；随后使用 Windows 已下载的模块缓存、GOPROXY=off 重跑并通过。正式日志对应最终代码。

golangci-lint 官方 v2.5.0 二进制基于 Go 1.25.1 构建，使用本机 Go 1.26 载入标准库时崩溃；临时选择匹配的 Go 1.25.1 后检查通过。未更改系统默认 Go 版本。

Python 的 typed 模式仍是补充模拟层，不能将其结果视为原版 python-snap7 服务端全部兼容；原版失败证据保留在此前报告中。

用户文件 `cmd/res/profiles/s7.kd048zb.profile.yml` 的 SHA256：`63609E8AB5FFD0905AC7B3AFB06869BF311FCA7B82985BFC281F4EB89BAC2C6A`。

## 证据

- [Go 1.25.1 测试、vet、构建](D:/workspace/iot/edgex/device-s7/doc/review/upstream-merge-native-results.txt)
- [最终 race](D:/workspace/iot/edgex/device-s7/doc/review/upstream-merge-race-results.txt)
- [最终 lint](D:/workspace/iot/edgex/device-s7/doc/review/upstream-merge-lint-results.txt)
- [Docker 尝试及取消记录](D:/workspace/iot/edgex/device-s7/doc/review/upstream-merge-docker-results.txt)
- [Python 三轮结果](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7/typed-results.txt)
- [原合并评估](D:/workspace/iot/edgex/device-s7/doc/review/device-s7-upstream-review-2026-09-30.md)

后续应在网络正常时重新执行 Docker 构建，并在实际 EdgeX 部署环境联调；本次基础验证不表示原 device-s7 审查报告的其它驱动问题已经全部修复。
