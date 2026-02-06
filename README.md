# office_find_item

Win7 32-bit 可用的“文件内容查找”工具（Go 单 exe）。支持 GUI（默认）与 CLI。

## 支持格式

- 文本类：`txt/md/log/csv/json/xml/ini/yaml/yml`
- Office OpenXML：`docx/xlsx/pptx/vsdx`（从压缩包内 XML 流式提取可见文本）
- 其它：`doc/xls/ppt/pdf` 通过 Windows `IFilter`（`LoadIFilter`）提取文本
  - 是否可用取决于系统是否安装了对应 IFilter：安装 **Office / WPS / PDF 阅读器（如 Acrobat/福昕等）** 通常即可

## PDF 重要说明（避免压测内存暴涨）

- **默认策略（推荐）**：Windows 下 PDF 依赖系统 IFilter（更省内存，稳定）。
- **内置 PDF 检索引擎（高风险）**：当系统没有可用 PDF IFilter 时，工具会自动启用“纯 Go fallback”解析 PDF（无需手动配置），但在部分 PDF 上可能导致 **内存/CPU 暴涨**。如果系统安装了 PDF IFilter，工具会自动优先使用更稳定的 IFilter。
  - GUI：状态栏显示 PDF IFilter 检测结果，可手动勾选“启用内置 PDF 检索引擎（可能导致内存暴涨）”强制使用纯 Go 引擎
  - CLI：环境变量 `OFIND_PDF_PUREGO` 可强制控制行为：`=1` 启用纯 Go 引擎，`=0` 禁用纯 Go 引擎（仅使用 IFilter）。未设置时自动检测。
  - 可选：`OFIND_PDF_MAX_FILE_BYTES` 控制纯 Go fallback 允许解析的 PDF 最大文件大小（默认 20MiB）
  - 可选：`OFIND_PDF_MAX_PAGES` 控制纯 Go fallback 允许解析的最大页数（默认 100 页），避免处理超大 PDF 时内存暴涨
  - 可选：`OFIND_PDF_PAGE_WORKERS` 控制 PDF 页面并行解析 worker 数（默认 1，关闭并行以避免内存暴涨）
  - 可选：`OFIND_MAX_ALLOC_MB` 控制单次分配内存硬限制（默认 32位 1200 MiB，64位 4096 MiB），超过则取消当前查询
  - 注意：内存硬限制现已始终生效（不再依赖调试模式）

## 使用（GUI）

- 运行：双击 `ofind.exe`（无参数时默认进入 UI），或执行：

```powershell
.\ofind.exe -ui
```

- Roots：可选择目录，也可“全盘”（多盘用 `;` 分隔）
- Query / Query2 / Query3：交集匹配（都命中才算命中）
- 停止输入约 400ms 后会自动开始搜索；双击结果会在资源管理器中定位文件；可导出 CSV 列表
- 状态栏会显示 `PDF IFilter` 检测结果，便于判断是否需要勾选“内置 PDF 检索引擎”

## 使用（CLI）

```powershell
# 基本用法
.\ofind.exe -roots "D:\Docs;E:\Work" -q "合同编号：A-001" -workers 8

# 交集匹配
.\ofind.exe -roots "D:\Docs" -q "关键字1" -q2 "关键字2"

# 搜索结束后在资源管理器中选中第 N 条结果（从 1 开始）
.\ofind.exe -roots "D:\Docs" -q "关键字" -open 1

# 若需要启用内置 PDF 引擎（不推荐，可能导致内存暴涨）
$env:OFIND_PDF_PUREGO=\"1\"
.\ofind.exe -roots \"D:\\Docs\" -q \"keyword\"
```

## 构建（Win7 32-bit 必读）

从 Go 1.21 起官方已移除 Windows 7 支持。要兼容 Win7（含 32-bit），请使用 Go 1.20.x（建议 1.20.14）进行构建。

可选方案：如果你本机装的是 Go 1.21+，也可以通过 `GOTOOLCHAIN=go1.20.14+auto` 让 `go` 自动下载并使用 1.20.14 工具链来编译（前提：你的 `go` 版本支持该机制）。

在 Windows 上：

```powershell
$env:CGO_ENABLED="0"
$env:GOOS="windows"
$env:GOARCH="386"

# 1) 控制台子系统（推荐：CLI 可见；UI 会自动隐藏黑框）
go build -trimpath -ldflags "-s -w" -o ofind.exe .\cmd\ofind

# 2) 纯 GUI 子系统（双击绝不弹控制台；但 CLI 输出不可见）
go build -trimpath -ldflags "-H=windowsgui -s -w" -o ofind.exe .\cmd\ofind
```
