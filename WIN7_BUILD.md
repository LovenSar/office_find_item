# Win7 32-bit 构建说明（必须）

你的报错：

```
Exception 0xc0000005 ... PC=0x0
runtime.asmstdcall
```

这类现象在 Win7 上非常典型：**不是代码逻辑崩了，而是 Go 运行时调用了 Win7 不存在/不可用的系统 API**。

从 Go 1.21 起官方已移除 Windows 7 支持（以及更早版本的 Windows），所以用较新的 Go 编译出来的 exe 在 Win7 上可能会直接闪退。

结论：
- **要支持 Win7 32-bit，必须用 Go 1.20.x（建议 1.20.14）或更老版本来编译。**

## 1) 安装 Go 1.20.x

任选一种：

- 方式 A：手动安装 go1.20.x（Windows msi 安装包）
- 方式 B：如果你用 scoop：安装/切换到 go@1.20（具体命令依你本地 scoop 包名为准）

验证：

```
go version
```

确保输出类似：

```
go version go1.20.14 windows/amd64
```

## 2) 静态（单 exe）构建命令

在项目根目录执行：

### 2.1 Win7 32-bit 控制台子系统（UI 会 FreeConsole 规避控制台窗口）

PowerShell：

```
$env:CGO_ENABLED="0"
$env:GOTOOLCHAIN="go1.20.14+auto"
$env:GOOS="windows"
$env:GOARCH="386"
go build -trimpath -ldflags "-s -w" -o ofind.exe .\cmd\ofind
```

### 2.2 Win7 32-bit 纯 GUI 子系统（双击绝不弹控制台，但 CLI 输出不可见）

```
$env:CGO_ENABLED="0"
$env:GOTOOLCHAIN="go1.20.14+auto"
$env:GOOS="windows"
$env:GOARCH="386"
go build -trimpath -ldflags "-H=windowsgui -s -w" -o ofind.exe .\cmd\ofind
```

> 说明：Windows 上所谓“静态链接”仍会依赖系统自带的 kernel32/user32/ole32 等 DLL（这是 Windows 机制决定的），但你拿到的仍然是单文件 exe，不需要额外运行时文件。

## 3) 运行自检

- 双击 `ofind.exe` 应该能打开 UI（Win7 上不闪退）
- 如果要跑命令行：建议用 2.1 的构建方式

如果你确认是用 Go 1.20.x 编译仍闪退，请把：
- `go version`
- 你使用的构建命令
- Win7 上运行时的完整崩溃输出

发我，我再继续往下排。
