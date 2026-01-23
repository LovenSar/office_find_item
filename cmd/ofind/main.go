package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"office_find_item/internal/app"
	"office_find_item/internal/winutil"
)

func main() {
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "Office Find Item - Win7 32-bit 文件内容搜索工具")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "用法:")
		fmt.Fprintln(out, "  ofind.exe -ui")
		fmt.Fprintln(out, "  ofind.exe -roots \"D:\\Docs;E:\\Work\" -q \"合同编号：A-001\" [-workers 8] [-open 1]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "参数:")
		flag.PrintDefaults()
		fmt.Fprintln(out)
		fmt.Fprintln(out, "说明:")
		fmt.Fprintln(out, "  - 默认支持 txt/md 等文本、docx/xlsx/pptx；doc/xls/ppt/pdf 通过系统 IFilter（需已安装对应组件）")
		fmt.Fprintln(out, "  - 结果可用 -open N 在资源管理器中选中")
	}
	flag.CommandLine.SetOutput(os.Stderr)

	// Windows 下双击启动通常不带参数：默认进入 UI。
	if runtime.GOOS == "windows" && len(os.Args) == 1 {
		// 尝试释放控制台（如果是以 console 子系统编译），避免拖着黑框。
		winutil.DetachConsole()
		if err := app.RunUI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// windowsgui 子系统下，CLI 需要显式 attach 到父控制台。
	isInternal := false
	isUI := false
	for _, a := range os.Args[1:] {
		switch a {
		case "-ui":
			isUI = true
		case "-worker", "-daemon":
			isInternal = true
		}
	}
	if runtime.GOOS == "windows" && !isUI && !isInternal {
		winutil.EnsureConsole()
	}

	var (
		ui      = flag.Bool("ui", false, "启动Windows UI")
		roots   = flag.String("roots", "", "要搜索的根目录，多个用 ; 分隔")
		query   = flag.String("q", "", "Query 1：要查找的字符串（Unicode）")
		query2  = flag.String("q2", "", "Query 2：要查找的字符串（交集）")
		query3  = flag.String("q3", "", "Query 3：要查找的字符串（交集）")
		workers = flag.Int("workers", 0, "并发工作线程数（默认=CPU核心数）")
		openIdx = flag.Int("open", 0, "搜索结束后打开第N个结果（从1开始），0表示不打开")
		worker  = flag.Bool("worker", false, "内部使用：作为子进程执行搜索并输出 JSON Lines")
		daemon  = flag.Bool("daemon", false, "内部使用：常驻索引+缓存进程（stdin 控制，stdout JSON Lines）")
	)
	flag.Parse()

	if *ui {
		if runtime.GOOS != "windows" {
			fmt.Fprintln(os.Stderr, "UI 仅支持 Windows")
			os.Exit(2)
		}
		if err := app.RunUI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *worker {
		if q := *query; len(q) == 0 {
			fmt.Fprintln(os.Stderr, "缺少 -q 参数")
			os.Exit(2)
		}
		if err := app.RunWorker(app.CLIOptions{
			Roots:   *roots,
			Query:   *query,
			Workers: *workers,
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *daemon {
		if runtime.GOOS != "windows" {
			fmt.Fprintln(os.Stderr, "daemon 仅支持 Windows")
			os.Exit(2)
		}
		if err := app.RunDaemon(app.CLIOptions{Roots: *roots, Workers: *workers}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if strings.TrimSpace(*query) == "" && strings.TrimSpace(*query2) == "" && strings.TrimSpace(*query3) == "" {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "错误：缺少查询参数（-q/-q2/-q3 至少一个）")
		os.Exit(2)
	}

	if err := app.RunCLI(app.CLIOptions{
		Roots:   *roots,
		Query:   *query,
		Query2:  *query2,
		Query3:  *query3,
		Workers: *workers,
		OpenIdx: *openIdx,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
