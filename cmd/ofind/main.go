package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"office_find_item/internal/app"
	"office_find_item/internal/winutil"
)

func main() {
	// 日志与崩溃回溯：写到 exe 同级目录，便于 Win7 回溯。
	if runtime.GOOS == "windows" {
		exe, _ := os.Executable()
		exeDir := filepath.Dir(exe)
		logPath := filepath.Join(exeDir, "ofind_debug.log")
		base := strings.ToLower(filepath.Base(exe))
		debugMode := strings.Contains(base, "debug") || os.Getenv("OFIND_DEBUG") == "1"

		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			if debugMode {
				log.SetOutput(io.MultiWriter(os.Stderr, f))
				// 监控内存变化与线程数
				go func() {
					ticker := time.NewTicker(2 * time.Second)
					var lastIO winutil.ProcessIOCounters
					var lastAt time.Time
					for range ticker.C {
						var m runtime.MemStats
						runtime.ReadMemStats(&m)

						ioStat, _ := winutil.GetProcessIOCounters()
						now := time.Now()
						dt := now.Sub(lastAt).Seconds()
						if lastAt.IsZero() || dt <= 0 {
							dt = 0
						}
						dRead := uint64(0)
						dWrite := uint64(0)
						if ioStat.ReadBytes >= lastIO.ReadBytes {
							dRead = ioStat.ReadBytes - lastIO.ReadBytes
						}
						if ioStat.WriteBytes >= lastIO.WriteBytes {
							dWrite = ioStat.WriteBytes - lastIO.WriteBytes
						}
						readRate := 0.0
						writeRate := 0.0
						if dt > 0 {
							readRate = float64(dRead) / 1024 / 1024 / dt
							writeRate = float64(dWrite) / 1024 / 1024 / dt
						}
						lastIO = ioStat
						lastAt = now

						log.Printf("[MONITOR] PID=%d | Goroutines=%d | Alloc=%.2f MiB | TotalAlloc=%.2f MiB | Sys=%.2f MiB | NumGC=%d | IO(R/W)=%.2f/%.2f MiB | IO(R/W)=%.2f/%.2f MiB/s",
							os.Getpid(), runtime.NumGoroutine(),
							float64(m.Alloc)/1024/1024,
							float64(m.TotalAlloc)/1024/1024,
							float64(m.Sys)/1024/1024,
							m.NumGC,
							float64(ioStat.ReadBytes)/1024/1024,
							float64(ioStat.WriteBytes)/1024/1024,
							readRate, writeRate)
					}
				}()
			} else {
				// release：不输出日志，也不弹窗
				log.SetOutput(io.Discard)
				f.Close()
			}
		}
		log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
		if debugMode {
			// debug：让子进程也显示控制台窗口，方便观察。
			_ = os.Setenv("OFIND_DEBUG_CONSOLE", "1")
		} else {
			_ = os.Unsetenv("OFIND_DEBUG_CONSOLE")
		}

		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("PANIC: %v\nStack:\n%s", r, debug.Stack())
				log.Println(msg)
				if debugMode {
					fmt.Fprintln(os.Stderr, msg)
					fmt.Println("\n程序发生关键错误，请截图反馈。按回车键退出...")
					var temp string
					fmt.Scanln(&temp)
					time.Sleep(10 * time.Second)
				}
			}
		}()
	}

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
		// release：分离控制台避免黑框；debug：保留控制台便于观察。
		exe, _ := os.Executable()
		base := strings.ToLower(filepath.Base(exe))
		debugMode := strings.Contains(base, "debug") || os.Getenv("OFIND_DEBUG") == "1"
		if !debugMode {
			winutil.DetachConsole()
		}
		log.Println("Starting UI mode...")
		if err := app.RunUI(); err != nil {
			log.Printf("UI Error: %v", err)
			if debugMode {
				fmt.Fprintln(os.Stderr, err)
			}
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
