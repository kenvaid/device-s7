# gos7 / python-snap7 内存区与混合类型集成测试

2026-09-30 已完成原版 python-snap7 3.2.0 和补充模拟层两种模式的实际 TCP 测试。原版的 M/I/Q 字节读写及并发通过；位操作、CT 读取、混合批量写入存在服务端实现缺口。补充模拟层下，本次全部测试连续三轮通过，无跳过项。本次没有修改 gos7 生产代码，也没有修改已安装的 python-snap7 包。

依赖发布后复验：device-s7 已移除本地 replace，使用远端 `v0.0.0-20260930084703-0a8e4f3e873c`。补充模式再次执行一轮，四项固定报文检查及全部 Go 集成测试通过，其中并发读写共 1600 次请求。下方补充模式日志链接已更新为这次远端模块的复验结果；原版失败日志保留。以下三轮统计描述首次本地工作区验证。

## 环境与测试数据

- Python 3.14，独立虚拟环境：`%TEMP%/gos7-python-simulator-venv`；固定安装 `python-snap7==3.2.0`。
- S7 服务：`127.0.0.1:1102`；独立的物理内存只读观察接口：`http://127.0.0.1:1103`。
- M、I、Q 各 8192 字节；CT/TM 各 2048 字节，即 1024 个双字节元素；DB1/DB2/DB3 各 8192 字节。
- gos7 使用 `D:/workspace/iot/edgex/gos7` 当前修复工作区，rack=0、slot=2；协商 PDU 上限 480 字节。
- 安装包服务端文件 SHA256（补充模式运行前后相同）：`30E5FFD28782E6906828437FE15E573E7694F4E35033FB842E80DF7639437908`。

该版本是独立的纯 Python 实现，不加载原生 Snap7 DLL。[PyPI 项目](https://pypi.org/project/python-snap7/3.2.0/)、[服务端 API](https://python-snap7.readthedocs.io/en/latest/API/server.html)

## 原版结果

| 场景 | 实际结果 |
| --- | --- |
| M/I/Q 字节读写 | 通过。非零起点 37，长度 1、3、16、452、462、463、480、1024、2048 字节，含跨 PDU 分包 |
| M/I/Q 位写入 | 失败。写入位值 1 后整个目标字节变为 01，相邻位丢失；通过服务端原始内存直接确认 |
| CT 非零索引写入 | 通过。起点为元素索引 7，直接核对物理偏移 14，包含 300 个元素跨包写入 |
| CT 读取 | 失败。服务端返回 BYTE/WORD/DWORD 的传输类型 04 与位长度；gos7 要求 Counter 的 OCTET 类型 09 与字节长度，因此正确拒绝该响应 |
| 混合 CHAR/REAL 批量写入 | 失败。7 个请求项仅返回一项写入确认，gos7 报 item count/length 错误；官方实现只解析首项写地址 |
| 单项越界写入 | 失败。服务端返回错误前已修改有效范围内的部分字节；直接内存观察确认偏移 8190 被写入 11 |
| 批量部分失败读取 | 通过。无效 CT 项后续有效的 M/Q 项仍能解析，失败项缓冲区不被发布覆盖 |
| M/I/Q 并发字节读写 | 通过。8 个 goroutine 共享连接和 8 个独立连接分别执行 800 次请求并回读比较 |

服务端源码与实验对应：`_handle_read_area/_handle_multi_read_area` 统一返回类型 04；`_parse_request_parameters` 的 WRITE 分支只解析首项；`_handle_write_area` 返回计数 1；位地址丢失位号并走整字节写入；`_write_to_memory_area` 先截断复制再判断写入完整性。[官方 3.2.0 服务端源码文档](https://python-snap7.readthedocs.io/en/latest/_modules/snap7/server.html)

原版并不能直接承担当前 gos7 的全部验收；这些失败没有被通过调整 gos7 解析规则隐藏。

## 补充模拟层结果

[typed_server.py](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7/typed_server.py) 继承 python-snap7 Server，沿用其 TPKT/COTP、会话与注册内存区，补充类型化读写处理：

- BIT 掩码读写保留其它位，返回 BIT 类型 03。
- CT/TM 按元素索引映射双字节存储，返回 OCTET 类型 09、字节长度。
- 按数据传输类型计算每项实际长度，完整处理多变量写入、奇数字节填充及逐项返回码。
- 校验完整报文后执行内存操作；单项越界在写入前拒绝，合法批量请求按项独立返回错误。

这是本地自行补充的模拟实现，其通过结果不能表述为“原版 python-snap7 已支持这些功能”，也不能替代真实 PLC 或未经修改的第三方实现互操作证据。传输类型及长度单位对照 [Snap7 协议实现](https://github.com/davenardella/snap7/blob/master/src/core/s7_micro_client.cpp)；另有不依赖 gos7 编码器的固定报文检查。

| 场景 | 补充模式结果 |
| --- | --- |
| M/I/Q 字节与分包 | 全部通过，起点 37，最大单次逻辑读写 2048 字节 |
| M/I/Q 位读写 | 三个区各覆盖非零字节偏移 17 的全部 8 位；相邻位和区内其它数据核对一致 |
| CT | 起点 7，1、3、225、226、227、300 个元素；每个元素两字节，BCD 0..999 数据；回读及物理地址校验通过 |
| 混合 CHAR/REAL | BYTE/CHAR/WORD/INT/DWORD/DINT/REAL 七种类型，跨 M/I/Q 区、三种项顺序；CHAR 为 3 字节奇数长度，REAL 包含 1.25、-12.5、12345.75；批量写入及回读通过 |
| CT 与普通区混合 | M 三字节、CT 三元素（索引 503）、Q 一字节，混合批量读写通过 |
| 越界与部分失败 | M/I/Q 单项越界拒绝且内存不变；批量中无效项不影响有效项结果；合法拒绝后连接仍可用 |
| 并发 | 每轮两种连接模式各 800 次读写；三轮合计 4800 次请求，全部回读一致 |

所有写入均通过 HTTP 观察接口直接比较服务端物理字节，检查预期地址和相邻字节，不只依靠 gos7 写入后再由同一库读取。CT 的独立预期偏移为 `2 * 元素索引`，可识别客户端和服务端使用相同错误地址而往返仍成功的问题。HTTP 观察用于功能核验，不用于性能计时。

此外，4 个固定十六进制报文检查均通过：计数器元素偏移与 OCTET 长度、位写保留相邻位、CHAR→REAL→BYTE 批量项定位、越界写不修改内存。测试文件为 [test_typed_server.py](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7/test_typed_server.py)，结果见 [固定报文日志](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7/typed-wire-vectors.txt)。

本次仅验证 CT 的原始协议数据和地址，不模拟 PLC 程序的计数脉冲、复位、扫描周期及定时器运行。TM/DB 已注册，但不属于本次新增集成测试的验收范围。

## 复跑与证据

运行补充模式，脚本自动启动独立实例、执行固定报文及三轮 Go 集成测试，并在结束时停止自己创建的模拟进程：

```powershell
pwsh.exe -File D:/workspace/iot/edgex/device-s7/doc/review/run-python-snap7-validation.ps1 -Mode typed -Count 3
```

复现原版缺口（测试预计返回非零退出码，失败不是跳过）：

```powershell
pwsh.exe -File D:/workspace/iot/edgex/device-s7/doc/review/run-python-snap7-validation.ps1 -Mode upstream -Count 1
```

脚本只使用本机 1102/1103 端口，检查端口空闲并输出 device-s7 实际使用的 gos7 版本和目录，支持远端模块及本地 replace。虚拟环境及 pip 安装不修改系统 Python 包；python-snap7 安装文件保持原样。测试数据属于独立临时模拟实例，进程退出即丢弃。用户的 ServerDemo 102 端口实例未被本轮测试修改。

新增 [Go 集成测试](D:/workspace/iot/edgex/device-s7/doc/review/pythonsim/integration_test.go) 默认无 `S7_PYTHON_ADDRESS` 时跳过；默认执行和 `go vet` 已通过。本次没有再次运行生产代码 race 回归，连接并发由上述实际 TCP 测试验证。

- [原版完整结果](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7/upstream-results.txt)
- [原版失败报文](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7/upstream-wire.txt)
- [补充模式最终三轮结果](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7/typed-results.txt)
- [服务端脚本](D:/workspace/iot/edgex/device-s7/doc/review/python-snap7/server.py)
