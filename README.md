# office_find_item

Win7 32-bit 可用的“文件内容查找”工具（Go 静态单 exe）。

## 支持格式

- 文本类：`txt/md/log/csv/json/xml/ini/yaml/yml`
- Office OpenXML：`docx/xlsx/pptx`（从压缩包内 XML 提取可见文本）
- 其它：`doc/xls/ppt/pdf` 通过 Windows `IFilter`（`LoadIFilter`）提取文本
  - 是否可用取决于系统是否安装了对应 IFilter（通常安装 Office / PDF 阅读器后具备）

## 构建（Win7 32-bit）

在 Windows 上：

```powershell
$env:GOOS="windows"; $env:GOARCH="386";
go build -ldflags "-s -w" -o ofind.exe .\cmd\ofind
```

## 使用

- CLI：

```powershell
.\ofind.exe -roots "D:\Docs;E:\Work" -q "合同编号：A-001" -workers 8
.\ofind.exe -roots "D:\Docs" -q "关键字" -open 1
```

- UI：

```powershell
.\ofind.exe -ui
```

双击结果会在资源管理器中选中文件。
