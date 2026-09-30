# gos7 / 本机 Snap7 ServerDemo 集成测试

补充验证：M/I/Q/CT 与混合 CHAR/REAL 的 python-snap7 测试已完成，原版结果及补充模拟层的三轮通过记录见 [Python 服务端验证](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7-validation-2026-09-30.md)。本记录中的 ServerDemo 未注册项仍保留其原始跳过结论。

2026-09-30 使用修复工作区实际连接用户运行中的 ServerDemo。追加修正后，连续三轮集成测试通过；M、I、Q、CT 因该实例未注册而跳过。测试结束后 DB1/DB2/DB3、测试涉及的 TM 字节及 CPU 状态均恢复并回读核验。未连接真实 PLC，未提交或推送代码。

## 测试对象

- 地址：`127.0.0.1:102`，rack=0、slot=2；监听进程为 `serverdemo.exe`，PID 12572。
- 模拟型号：CPU 315-2 PN/DP，订货号 `6ES7 315-2EH14-0AB0`，固件字段 3.2.6；这些是模拟器报告值。
- 协商 PDU 为 480 字节。CP 信息另报告最大 PDU 2048、最大连接数 1024；本次未验证这些上限。
- DB1、DB2、DB3 各 2048 字节；TM 区可用，M/I/Q/CT 返回 item error 0A。
- Go：1.26.4 Windows/amd64。device-s7 经 `replace ../gos7` 引用 `D:/workspace/iot/edgex/gos7` 当前未提交修复代码；最终测试未使用 overlay。
- ServerDemo SHA256：`D6468135B202DA6B071D5DFFE7A367A683E52017BDC76FE0556854096A9F3095`。
- snap7.dll SHA256：`161623AAA3EE8081244A0A4EE01D27E9081FBCA057C9D51CC57012CD801DA44E`。

## 测试结果

| 范围 | 结果与实际覆盖 |
| --- | --- |
| 连接、重连 | 握手与 PDU 协商成功；每轮关闭、重连并读取 5 次 |
| DB 读写 | 三个 DB 均验证 1、3、16、452、462、463、480、1024、2048 字节往返，覆盖跨 PDU 分包 |
| 位寻址 | DB1 字节偏移 17 的全部 8 位验证读写，核对相邻位未被修改 |
| 多变量 | 20 项读写、奇数长度填充、BYTE/CHAR/WORD/INT/DWORD/DINT/REAL 批量读取通过；CHAR/REAL 写入分别用单项请求验证，见下方模拟器限制 |
| DB 管理 | DBFill、DBGet、DB 块信息和 DBW 地址读取通过 |
| 拒绝响应 | 不存在 DB、DB 越界、批量中的部分失败正确返回；失败项缓冲区保留原值，连接继续可用 |
| 单连接并发 | 8 个 goroutine 共享连接，各执行 100 次 32 字节写入、回读与逐字节比较，三轮无串包或数据不符 |
| 多连接并发 | 8 个独立连接，同样执行读写回读校验，三轮全部通过；每个 worker 使用独立地址范围 |
| TM | 从索引 0 读写 8 个元素，每元素 2 个协议字节；未覆盖非零索引和跨 PDU 定时器语义 |
| M/I/Q/CT | 实例未注册，明确跳过；不能据此声明这些内存区已经完成独立模拟器验证 |
| 系统信息 | 订货号、CPU/CP 信息、保护字段、CPU 状态、时钟读取及目录查询通过；目录返回 DB1/DB2/DB3 |
| 管理写入 | 会话密码设置/清除、设置时钟确认、STOP→热启动→STOP→冷启动及状态查询通过；原 CPU 状态 RUN 已恢复 |

并发部分共执行 9,600 次请求。三轮单连接请求速率分别为 11,064、11,867、10,934 次/秒，8 个连接为 75,779、73,301、75,431 次/秒。一次写入和一次读取分别算一个请求。以上是短时本机功能测试的计时，受 GUI 日志、系统负载与计时精度影响，不能作为真实 PLC 吞吐能力或稳定性能基准。单连接仍串行交换，多连接可并行。

## 此次追加修复

首次直接连接发现此前构造报文回归没有覆盖的五类兼容性问题。已按此前用户授权写入实际 gos7 工作区，并增加独立报文回归：

1. GET 时钟响应允许 `0x20` 字段；对 Snap7 的 0..6 星期表示仅在日历日期一致时兼容。公开 DATE_AND_TIME Helper 和 SET 编码仍使用严格的 1..7 表示。
2. 列举某类块首次返回 `D20E` 时视为空目录，继续查询其他类型；其它错误和续片失败仍传播。
3. 失败读项允许无载荷的长度字段 0 或 4，返回逐项错误并保留会话；任意其它长度仍拒绝。
4. 奇数长度项的填充验证存在，不要求值为零；缺失填充和报文边界错误仍拒绝。
5. 校时及密码操作接受精确的空确认 `0A 00 00 00`，也保留 `FF 09 00 00`；读取接口不接受空确认作为有效数据。

生产文件：[datetime.go](D:/workspace/iot/edgex/gos7/datetime.go)、[directory.go](D:/workspace/iot/edgex/gos7/directory.go)、[multi.go](D:/workspace/iot/edgex/gos7/multi.go)、[protocol.go](D:/workspace/iot/edgex/gos7/protocol.go)、[security.go](D:/workspace/iot/edgex/gos7/security.go)。新增回归：[serverdemo_regression_test.go](D:/workspace/iot/edgex/gos7/serverdemo_regression_test.go)。

## 模拟器限制

实际混合 CHAR/REAL 多项写入返回 data size mismatch。核对官方服务端源码，批量定位后续写项时按变量类型决定长度单位，CHAR/REAL 的字节长度被除以 8，导致后续项位置错误。测试保留原始失败报文，采用其它类型的批量写入、CHAR/REAL 单项写入、所有类型混合批量读取进行互操作验证；未为迎合模拟器修改 gos7 的合法类型编码。依据为 [Snap7 服务端实现](https://github.com/davenardella/snap7/blob/master/src/core/s7_server.cpp) 的 PerformFunctionWrite/WriteArea。

同一源码的 FillTime、RA_NotFound 和 PerformGroupSecurity/PerformSetClock 与本次抓到的星期、错误项及空确认一致。模拟器不执行 PLC 程序，密码操作不验证实际权限，SET 时钟也不改变系统时钟；管理写入成功只证明报文互操作。[官方服务端说明](https://snap7.sourceforge.net/snap7_server.html)

## 回归与复跑

实际两个仓库的 `go test -count=1 -cover ./...`、`go vet ./...`、`go build ./...` 均以退出码 0 完成。gos7 主包语句覆盖率 74.9%；device-s7 驱动包 14.4%。WSL 下两个仓库的 `go test -race -count=1 -timeout=90s ./...` 也通过；这里的 race 是本地回归，未对 Windows ServerDemo 的连接测试启用 race。

初次 WSL 调用在沙箱返回 E_ACCESSDENIED，随后系统环境调用成功。原生检查中出现模块 stat cache 写入被拒绝的缓存提示，命令退出码仍为 0。相关失败调用日志单独保留，不作为最终验证结果。

保持 ServerDemo 在本机 102 端口运行后执行：

```powershell
pwsh.exe -File D:/workspace/iot/edgex/device-s7/doc/review/run-serverdemo-validation.ps1 -Count 3
```

脚本校验端口归属，输出实际 gos7 版本和目录（支持远端模块及本地 replace），保存 DB 恢复数据，并执行 [集成测试](D:/workspace/iot/edgex/device-s7/doc/review/serverdemo/serverdemo_test.go)。普通 `go test ./...` 不设置环境变量时跳过外部服务端测试。

- [最终三轮日志](D:/workspace/iot/edgex/device-s7/doc/review/serverdemo-final-results.txt)
- [原生回归日志](D:/workspace/iot/edgex/device-s7/doc/review/serverdemo-native-regression-results.txt)
- [初始失败日志](D:/workspace/iot/edgex/device-s7/doc/review/serverdemo-initial-results.txt)
- [初始读写报文](D:/workspace/iot/edgex/device-s7/doc/review/serverdemo-wire-initial.txt)
- [管理接口报文](D:/workspace/iot/edgex/device-s7/doc/review/serverdemo-management-wire.txt)
- [本轮 DB 恢复数据](D:/workspace/iot/edgex/device-s7/doc/review/serverdemo-original-db-20260930-154301.json)
