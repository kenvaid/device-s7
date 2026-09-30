# gos7 F01–F20 修复闭环记录

2026-09-30 更新：已使用本机官方 Snap7 ServerDemo 完成独立服务端集成验证，追加五类报文兼容性修复；测试结果、跳过项及模拟器限制见 [ServerDemo 验证记录](D:/workspace/iot/edgex/device-s7/doc/review/serverdemo-validation-2026-09-30.md)。以下内容保留 2026-09-29 验证时的历史状态。

基线：`b01c5bde747688ce137d19ea44723ec0e7662e57`。修复工作区：`D:/workspace/iot/edgex/gos7`，分支 `codex/fix-s7-protocol`。本次范围为 [gos7 审查报告](D:/workspace/iot/edgex/device-s7/doc/review/gos7-review-2026-09-29.md) 的 F01–F20，以及报告要求的 device-s7 位地址联动。

生产代码和正式回归测试均已写入实际仓库。2026-09-30，修复提交 `0a8e4f3e873cd8ef2d6c10e38282418fa31dde46` 已合并并推送至 kenvaid/gos7 的 master。device-s7 的 [go.mod](D:/workspace/iot/edgex/device-s7/go.mod) 已更新为 `v0.0.0-20260930084703-0a8e4f3e873c`，移除本地 replace，实际构建使用远端模块。用户已有的 profile 修改未作改动。

## 逐项验收

| 编号 | 实施内容 | 正式测试证据 |
| --- | --- | --- |
| F01 | 为请求分配 Reference，严格验证 TPKT/COTP/S7、功能及响应关联；超时、EOF、非法响应清理原会话；新请求必须显式重连。清理函数绑定产生响应的连接，延迟解析不能关闭新连接。 | `TestResponseErrorAndMatching`、`TestProtocolIdentityMismatch`、`TestTimeoutDropsSessionAndCloseInterrupts`、`TestFaultedSessionRequiresFreshHandshake`、`TestDelayedValidationCannotCloseNewSession` |
| F02 | S7DataItem 的 Start 为字节偏移、Bit 为 0..7；位读写共同编码 Start*8+Bit。device-s7 DBX 不再预乘 8，保留独立 Bit，DB/M/I/Q/V 均覆盖。 | `TestMultiAddressAndLengths`；driver 的 `TestS7AddressByteAndBitContract` |
| F03 | 同一连接的完整握手和收发共享交换边界；Connect 幂等；PDUSize、Reference、连接状态受锁保护；Close 可以中断 I/O。闲置定时器用独立代数防止旧回调关闭活动会话。 | `TestConcurrentConnectAndReadWrite`、`TestConnectContextCancelsHandshakeAndQueuedConnect`、`TestIdleTimeoutAndActiveExchange`；两个项目的 race 检查 |
| F04 | 统一按 S7 报文类型定位全局错误；返回具体 S7Error，成功字段为 nil；合法 CPU 拒绝响应保留连接。 | `TestResponseErrorAndMatching` |
| F05 | 使用真实接收长度，不再暴露填零的容量；验证全帧和逐项长度、类型、数量、填充，以及所有管理接口实际访问边界；完整解析后才发布批量读取结果。 | `TestMalformedMultiNeverPublishes`、`TestManagementRejectsTruncatedData`、`FuzzProtocolParsers` |
| F06 | I/O 前检查 count、amount、DB/区域/位号/地址范围、buffer、PDU 双向预算及 16 位长度字段；拒绝短写和溢出。 | `TestValidationBeforeIO`、`TestTransportLengthFieldCannotWrap`、`TestAddressSyntax` |
| F07 | SET 的八字节时间字段从偏移 31 写入；GET 首先传播网络错误。新增 ClockClient.GetPLCDateTime/SetPLCDateTime；旧名称保留历史语义并标记 Deprecated。 | `TestClockProtectionCPStatusAndBlockInfo`、`TestGenericTransportAndClockError` |
| F08 | Counter/Timer 使用共同的 OCTET 传输尺寸和字节长度，Counter 不再落入空 case。 | `TestWriteTransportGoldenVectors` |
| F09 | CT/TM 快捷接口完整接受、返回每元素两个大端协议字节；跨包时按元素索引推进地址。 | `TestWriteTransportGoldenVectors`、`TestSingleAreaChunkingAndRawCTTM` |
| F10 | SZL 逐片追加，保留记录宽度，返回实际记录总长；后续片从 UserData 载荷起始位置拼接；验证身份、ReturnCode、错误字段、分片标记和总长度。 | `TestSZLFragmentAndOrderCode`、`TestManagementRejectsTruncatedData` |
| F11 | 订货号读取 SZL 0011；按 28 字节记录的 ID 1 读取模块码、ID 7 读取固件版本；检查记录完整性。CPU 信息同样按 SZL 001C 的记录 ID 读取，避免依赖记录排序。 | `TestSZLFragmentAndOrderCode`、`TestCPUInfoRecordIdentity` |
| F12 | 独立解析 DB、I/E、Q/O/A、M、C/Z、T；完整检查数字、范围及段数；未支持语法返回错误。 | `TestAddressSyntax` |
| F13 | TCP 协商接受 240..请求值 480；未完成会话拒绝业务操作；预算排除七字节 ISO 头，兼顾请求和响应；分包有正向进度并受传输长度字段限制。 | `TestNegotiatedPDUBoundaries`、`TestSingleAreaChunkingAndRawCTTM`、`TestTransportLengthFieldCannotWrap` |
| F14 | WSTRING 按 UCS-2 字符数写 WORD 头，拒绝不能表达的字符和非法头；严格接口拒绝超长，旧接口按字符截断。 | `TestHelperProtocolBoundaries`、`FuzzHelpers` |
| F15 | Counter 正确编码为三位 BCD 的大端 WORD，范围 0..999，拒绝非法 BCD。 | `TestHelperProtocolBoundaries`，穷举 0..999 往返并验证 123→0123 |
| F16 | SetCharsAt 做边界检查后原位 copy，不插入或重新分配 slice。 | `TestHelperProtocolBoundaries`、`TestHelperBoundsAndAtomicWrites` |
| F17 | DATE 为 uint16 天数；TOD 使用 pos 并保留毫秒；LTOD/DTL/LDT 保留纳秒；DT/DTL 星期为 1..7；修正 S5TIME 端点；新增 Checked 方法传播编码/范围/缓冲区错误。 | `TestHelperProtocolBoundaries`、`TestHelperBoundsAndAtomicWrites`、`FuzzHelpers` |
| F18 | 目录遍历每个块类型一次，首次错误即返回，移除调试打印；校验四字节目录记录并支持分片续读。 | `TestDirectoryAndSecurityControl`、`TestManagementRejectsTruncatedData` |
| F19 | TCPClient 返回包含连接生命周期和清晰时钟接口的 ConnectedClient；保持基础 Client 接口；自定义 Transporter 通过可选 PDUSizeProvider/SessionTransporter 提供能力，不再依赖 TCP 具体类型断言。 | `TestPublicInterfaces`、`TestGenericTransportAndClockError` |
| F20 | S7Protection 的五个字段导出，并检查保护记录边界。 | 外部包 `TestPublicInterfaces`、`TestClockProtectionCPStatusAndBlockInfo` |

主要实现：[protocol.go](D:/workspace/iot/edgex/gos7/protocol.go)、[tcpclient.go](D:/workspace/iot/edgex/gos7/tcpclient.go)、[client.go](D:/workspace/iot/edgex/gos7/client.go)、[multi.go](D:/workspace/iot/edgex/gos7/multi.go)、[helper.go](D:/workspace/iot/edgex/gos7/helper.go)、[system.go](D:/workspace/iot/edgex/gos7/system.go)、[device-s7 driver.go](D:/workspace/iot/edgex/device-s7/internal/driver/driver.go)。迁移及使用约定见 [gos7 README](D:/workspace/iot/edgex/gos7/README.md)。

正式测试：[协议与生命周期回归](D:/workspace/iot/edgex/gos7/protocol_regression_test.go)、[Helper 回归](D:/workspace/iot/edgex/gos7/helper_regression_test.go)、[外部接口回归](D:/workspace/iot/edgex/gos7/api_regression_test.go)、[驱动地址回归](D:/workspace/iot/edgex/device-s7/internal/driver/driver_test.go)。

## 验证与复跑

执行以下命令，同时验证两个实际仓库；不是仅编译临时副本：

```powershell
pwsh.exe -File D:/workspace/iot/edgex/device-s7/doc/review/run-gos7-fix-validation.ps1 -Race -Fuzz
```

验证内容为两个项目的 `go test -count=1 -cover ./...`、`go vet ./...`、`go build ./...`、WSL 下的 `go test -race -count=1 ./...`，以及每个 5 秒、两个 worker 的 Helper/协议解析 fuzz。使用本机 Go 1.26.4；库声明的最低版本仍为 Go 1.21，未另行运行最低版本测试。

结果保存在 [最终验证日志](D:/workspace/iot/edgex/device-s7/doc/review/gos7-fix-validation-results.txt)。fuzz 是短时边界探索，不能视为穷尽验证；语句覆盖率也不代表设备兼容性或所有协议变体覆盖率。

最终结果：两个仓库的测试、vet、build、race **全部通过**；gos7 主包覆盖率 **74.0%**（审查基线 10.4%），device-s7 驱动包覆盖率 14.4%。最后一轮 Helper fuzz 执行 282,823 次、协议解析 fuzz 执行 288,237 次，均通过。`go list -m` 确认 device-s7 实际解析到 `D:/workspace/iot/edgex/gos7`。

原审查的 overlay 测试及日志保留为基线证据。旧测试依赖已替换的私有实现并使用部分简化报文，不再作为当前版本的验收入口。新正式回归报文显式编码实际 TPKT、COTP、S7 长度和 Reference；传输尺寸另有固定向量，避免只靠库自己的编码器验证自己。

## 协议依据及仍需设备验证的范围

- 传输长度单位和非末项的奇数字节填充，对照 [Wireshark S7 dissector](https://github.com/wireshark/wireshark/blob/master/epan/dissectors/packet-s7comm.c)；报文结构对照 [Apache PLC4X S7 协议定义](https://github.com/apache/plc4x/blob/develop/protocols/s7/src/main/resources/protocols/s7/s7.mspec)。
- SZL 记录 ID/宽度按西门子的 [模块标识 0011](https://docs.tia.siemens.cloud/r/en-us/v20/extended-instructions-s7-300-s7-400/diagnostics-s7-300-s7-400/system-status-list-s7-300-s7-400/szl-id-w-16-xy11-module-identification-s7-300-s7-400)及 [组件标识 001C](https://docs.tia.siemens.cloud/r/en-us/v20/extended-instructions-s7-300-s7-400/diagnostics-s7-300-s7-400/system-status-list-s7-300-s7-400/szl-id-w-16-xy1c-component-identification-s7-300-s7-400)定义解析；SZL 续片、时钟布局与目录续读同时核对 [Snap7 类型定义](https://github.com/SCADACS/snap7/blob/master/src/core/s7_types.h)和 [协议实现](https://github.com/SCADACS/snap7/blob/master/src/core/s7_micro_client.cpp)，修复代码为独立实现。
- 日期、时间和字符串按 [西门子支持的数据类型](https://docs.tia.siemens.cloud/r/simatic_s7_1500_et_200mp_manual_collection_enus_21/comprehensive-information/cpu-function-manuals/web-server/api-application-programming-interface/reading-and-writing-process-data/supported-data-types)、[DATE_AND_TIME](https://docs.tia.siemens.cloud/r/en-us/v20/data-types/date-and-time/date_and_time-date-and-time-of-day)、[DTL](https://docs.tia.siemens.cloud/r/en-us/v20/data-types/date-and-time/dtl-s7-1200-s7-1500)和 [LDT](https://docs.tia.siemens.cloud/r/en-us/v20/data-types/date-and-time/ldt-date_and_ltime-s7-1500)的范围和单位处理。

本次只访问本机模拟 PLC，没有向真实设备执行写入、校时或启停。还需用目标 PLC 型号验证读写、保护级别、管理接口支持差异和异常重连，并测量吞吐量。单连接仍只有一个在途请求，独立连接可以并发；未新增多 Job 流水线或保证多包事务原子性。MPI/PPI 仍是原项目明确标注的未实现功能。本记录不表示之前 device-s7 独立审查报告中的其它驱动问题也已全部修复。
