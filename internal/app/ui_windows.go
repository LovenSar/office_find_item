//go:build windows

package app

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/lxn/walk"
	"github.com/lxn/walk/declarative"
	_ "office_find_item/internal/extract"
	"office_find_item/internal/winutil"
)

func RunUI() error {

	var (
		mw          *walk.MainWindow
		rootsEdit   *walk.LineEdit
		queryEdit   *walk.LineEdit
		query2Edit  *walk.LineEdit
		query3Edit  *walk.LineEdit
		pdfPureGoCB *walk.CheckBox
		status      *walk.Label
		btnStop     *walk.PushButton
		tableView   *walk.TableView

		daemonMu   sync.Mutex
		daemons    map[string]*daemonProcess
		rootsKey   string
		pdfPureKey bool
		debounceMu sync.Mutex
		debounceT  *time.Timer
		gen        uint64
		closeOnce  sync.Once
		closeCh    = make(chan struct{})
		uiClosed   uint32

		model = NewResultsModel()

		// 结果缓冲通道，避免大量 Synchronize 导致 UI 闪退
		resultCh = make(chan daemonOut, 2000)
	)

	pdfIFilterOK := false // 临时禁用 IFilter 检测以修复 UI 启动问题

	statusSuffix := func() string {
		pdfEngine := "OFF"
		if pdfPureGoCB != nil && pdfPureGoCB.Checked() {
			pdfEngine = "ON"
		}
		if pdfIFilterOK {
			return fmt.Sprintf("PDF IFilter: 有 | 内置PDF: %s", pdfEngine)
		}
		return fmt.Sprintf("PDF IFilter: 未检测到 | 内置PDF: %s", pdfEngine)
	}

	setStatus := func(msg string) {
		if status == nil {
			return
		}
		sfx := statusSuffix()
		if strings.TrimSpace(msg) == "" {
			status.SetText(sfx)
			return
		}
		status.SetText(msg + " | " + sfx)
	}

	countQueryChars := func(q string) (asciiCount int, unicodeCount int) {
		for _, r := range strings.TrimSpace(q) {
			if unicode.IsSpace(r) {
				continue
			}
			if r <= 0x7f {
				asciiCount++
			} else {
				unicodeCount++
			}
		}
		return asciiCount, unicodeCount
	}

	queryIsSearchable := func(q string) bool {
		asciiCount, unicodeCount := countQueryChars(q)
		return asciiCount >= 3 || unicodeCount >= 2
	}

	clearSelection := func() {
		if tableView == nil {
			return
		}
		_ = tableView.SetSelectedIndexes(nil)
		_ = tableView.SetCurrentIndex(-1)
	}

	forceTableRefresh := func() {
		if tableView == nil {
			return
		}
		// 某些环境下仅 PublishRowsReset 仍会残留旧行显示，重绑模型可彻底刷新。
		_ = tableView.SetModel(nil)
		_ = tableView.SetModel(model)
		_ = tableView.StretchLastColumn()
	}

	stopSearch := func() {
		debounceMu.Lock()
		if debounceT != nil {
			debounceT.Stop()
			debounceT = nil
		}
		gen++
		myGen := gen
		debounceMu.Unlock()

		daemonMu.Lock()
		for _, d := range daemons {
			_ = d.SetQuery("", "", "", myGen, 30, 3)
		}
		daemonMu.Unlock()
		clearSelection()
		forceTableRefresh()
		setStatus("已取消，等待输入...")
		if btnStop != nil {
			btnStop.SetEnabled(false)
		}
	}

	revealSelected := func() {
		idx := tableView.CurrentIndex()
		row, ok := model.Row(idx)
		if !ok {
			return
		}
		_ = winutil.RevealInExplorer(row.Path)
	}

	startSearchNow := func(q1 string, q2 string, q3 string) {
		q1 = strings.TrimSpace(q1)
		q2 = strings.TrimSpace(q2)
		q3 = strings.TrimSpace(q3)
		if q1 == "" && q2 == "" && q3 == "" {
			stopSearch()
			model.Reset()
			setStatus("Ready")
			return
		}
		if (q1 != "" && !queryIsSearchable(q1)) || (q2 != "" && !queryIsSearchable(q2)) || (q3 != "" && !queryIsSearchable(q3)) {
			setStatus("输入太短：至少3个ASCII字符或2个Unicode字符才开始搜索")
			return
		}
		roots := strings.TrimSpace(rootsEdit.Text())
		if roots == "" {
			setStatus("请先选择目录或全盘")
			return
		}

		// cancel previous query (but keep daemons)
		model.Reset()
		clearSelection()
		forceTableRefresh()
		btnStop.SetEnabled(true)
		setStatus("Searching...")

		// generation guard
		debounceMu.Lock()
		gen++
		myGen := gen
		debounceMu.Unlock()

		exePath, _ := os.Executable()
		rawParts := strings.Split(roots, ";")
		parts := make([]string, 0, len(rawParts))
		for _, p := range rawParts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			parts = append(parts, p)
		}
		nextKey := strings.Join(parts, ";")
		enablePureGoPDF := false
		if pdfPureGoCB != nil {
			enablePureGoPDF = pdfPureGoCB.Checked()
		}

		daemonMu.Lock()
		if daemons == nil {
			daemons = map[string]*daemonProcess{}
		}
		if rootsKey != nextKey || pdfPureKey != enablePureGoPDF {
			for _, d := range daemons {
				d.Close()
			}
			daemons = map[string]*daemonProcess{}
			rootsKey = nextKey
			pdfPureKey = enablePureGoPDF
		}
		for _, root := range parts {
			if root == "" {
				continue
			}
			if _, ok := daemons[root]; ok {
				continue
			}
			dproc, err := startDaemonProcess(exePath, root, 0, enablePureGoPDF, func(out daemonOut) {
				// 发送到聚合通道，由单独协程批量刷入 UI
				if atomic.LoadUint32(&uiClosed) != 0 {
					return
				}
				select {
				case resultCh <- out:
				default:
					// 通道满则丢弃（极少发生）
				}
			})
			if err == nil {
				daemons[root] = dproc
			}
		}
		// send query to all
		for _, d := range daemons {
			_ = d.SetQuery(q1, q2, q3, myGen, 30, 3)
		}
		daemonMu.Unlock()
	}

	scheduleSearch := func() {
		debounceMu.Lock()
		if debounceT != nil {
			debounceT.Stop()
		}
		debounceT = time.AfterFunc(400*time.Millisecond, func() {
			mw.Synchronize(func() {
				startSearchNow(queryEdit.Text(), query2Edit.Text(), query3Edit.Text())
			})
		})
		debounceMu.Unlock()
	}

	mwDecl := declarative.MainWindow{
		AssignTo: &mw,
		Title:    "Office Find Item",
		MinSize:  declarative.Size{Width: 820, Height: 520},
		Layout:   declarative.VBox{},
		Children: []declarative.Widget{
			declarative.Composite{
				Layout: declarative.Grid{Columns: 7},
				Children: []declarative.Widget{
					declarative.Label{Text: "Roots"},
					declarative.LineEdit{AssignTo: &rootsEdit, ColumnSpan: 3},
					declarative.PushButton{
						Text: "选择...",
						OnClicked: func() {
							dlg := new(walk.FileDialog)
							if ok, _ := dlg.ShowBrowseFolder(mw); ok {
								rootsEdit.SetText(dlg.FilePath)
							}
						},
					},
					declarative.PushButton{
						Text: "桌面",
						OnClicked: func() {
							if p, err := winutil.DesktopDir(); err == nil && strings.TrimSpace(p) != "" {
								rootsEdit.SetText(p)
							} else {
								// fallback: 保留现有 roots
								setStatus("无法获取桌面目录")
							}
						},
					},
					declarative.PushButton{
						Text: "全盘",
						OnClicked: func() {
							ret := walk.MsgBox(mw, "提示", "全盘搜索可能需要很长时间，确定吗？", walk.MsgBoxYesNo|walk.MsgBoxIconWarning)
							if ret == walk.DlgCmdYes {
								rootsEdit.SetText(strings.Join(winutil.ListSearchableDrives(), ";"))
							}
						},
					},
				},
			},
			declarative.Composite{
				Layout: declarative.Grid{Columns: 6},
				Children: []declarative.Widget{
					declarative.Label{Text: "Query"},
					declarative.LineEdit{AssignTo: &queryEdit},
					declarative.Label{Text: "Query 2"},
					declarative.LineEdit{AssignTo: &query2Edit},
					declarative.Label{Text: "Query 3"},
					declarative.LineEdit{AssignTo: &query3Edit},
					declarative.CheckBox{
						AssignTo:   &pdfPureGoCB,
						Text:       "启用内置 PDF 检索引擎（可能导致内存暴涨）",
						Checked:    false,
						ColumnSpan: 6,
						OnCheckedChanged: func() {
							// 变更需要重启 daemon 才能生效（通过 env 控制）
							stopSearch()
						},
					},
					declarative.Label{Text: "建议安装 Office / PDF 阅读器 / WPS（提供 PDF IFilter），更省内存更稳定。", ColumnSpan: 6},
					declarative.PushButton{AssignTo: &btnStop, Text: "停止", Enabled: false, OnClicked: stopSearch, ColumnSpan: 5},
					declarative.PushButton{
						Text: "导出列表",
						OnClicked: func() {
							if model.RowCount() == 0 {
								walk.MsgBox(mw, "提示", "没有结果可导出", walk.MsgBoxIconInformation)
								return
							}
							dlg := new(walk.FileDialog)
							dlg.Filter = "CSV Files (*.csv)|*.csv"
							dlg.Title = "导出结果"
							if ok, _ := dlg.ShowSave(mw); ok {
								f, err := os.Create(dlg.FilePath)
								if err != nil {
									walk.MsgBox(mw, "错误", err.Error(), walk.MsgBoxIconError)
									return
								}
								defer f.Close()
								f.WriteString("\xEF\xBB\xBF") // BOM
								w := csv.NewWriter(f)
								w.Write([]string{"#", "Path", "Extension", "Context", "Size", "Modified"})
								for i, r := range model.rows {
									w.Write([]string{
										fmt.Sprintf("%d", i+1),
										r.Path,
										r.Extension,
										r.Snippet,
										r.Size,
										r.ModTime,
									})
								}
								w.Flush()
								walk.MsgBox(mw, "成功", "导出完成", walk.MsgBoxIconInformation)
							}
						},
						ColumnSpan: 1,
					},
					declarative.Label{Text: "提示：在上方 Query/Query2/Query3 输入关键词（交集匹配），停止输入约 400ms 后会自动开始搜索；双击结果可在资源管理器中定位。", ColumnSpan: 6},
				},
			},
			declarative.Label{AssignTo: &status, Text: "Ready（输入后自动搜索）"},
			declarative.TableView{
				AssignTo: &tableView,
				Model:    model,
				// LastColumnStretched: true, // 用户反馈会导致出现横向滚动条，移除自动拉伸
				ColumnsSizable: true,
				// DoubleBuffering:     true, // 双缓冲在部分 Win7 环境下可能导致内容不显示，暂关闭
				Columns: []declarative.TableViewColumn{
					{Title: "#", Width: 35},
					{Title: "Path", Width: 280},
					{Title: "Ext", Width: 45},
					{Title: "Context", Width: 280},
					{Title: "Size", Width: 60, Alignment: declarative.AlignFar},
					{Title: "Modified", Width: 120},
				},
				OnItemActivated: revealSelected,
			},
		},
	}

	if err := mwDecl.Create(); err != nil {
		return err
	}
	setStatus("Ready（输入后自动搜索）")

	// UI 结果聚合协程：
	// - 合并刷新，避免每条结果都 Synchronize 导致 UI 卡顿
	// - 不干预用户滚动/拖动，避免“盲点双击、延迟出现”的体验
	go func() {
		const maxBufferItems = 20000
		buffer := make([]daemonOut, 0, 2048)
		// 小批量刷新：避免一次性处理太多导致 UI 线程长时间阻塞（表现为白屏/无响应）
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-closeCh:
				return
			case out := <-resultCh:
				if atomic.LoadUint32(&uiClosed) != 0 {
					continue
				}
				if len(buffer) < maxBufferItems {
					buffer = append(buffer, out)
				} else {
					// buffer 满了，丢弃后续数据以防 32 位内存爆掉
				}
			case <-ticker.C:
				if atomic.LoadUint32(&uiClosed) != 0 {
					return
				}
				if mw == nil || len(buffer) == 0 {
					continue
				}
				// 只取一小段批次，保证每次 UI 刷新足够快
				const maxBatchPerTick = 400
				n := len(buffer)
				if n > maxBatchPerTick {
					n = maxBatchPerTick
				}
				batch := make([]daemonOut, n)
				copy(batch, buffer[:n])
				buffer = buffer[n:]

				mw.Synchronize(func() {
					if atomic.LoadUint32(&uiClosed) != 0 {
						return
					}
					// 获取当前 valid 的 queryID
					debounceMu.Lock()
					curGen := gen
					debounceMu.Unlock()

					rowsToAdd := make([]ResultRow, 0, 256)
					var lastStatusMsg string
					var isDone bool
					start := time.Now()
					const maxAppendPerTick = 200
					const maxUITick = 25 * time.Millisecond

					for _, out := range batch {
						if out.QueryID != curGen {
							continue
						}
						switch out.Type {
						case "result":
							if len(rowsToAdd) >= maxAppendPerTick {
								continue
							}
							if time.Since(start) > maxUITick {
								continue
							}
							snip := strings.Join(out.Snippets, "  |  ")
							rowsToAdd = append(rowsToAdd, ResultRow{
								Path:      out.Path,
								Snippet:   snip,
								Extension: out.Extension,
								Size:      formatSize(out.Size),
								ModTime:   time.Unix(out.ModTime, 0).Format("2006-01-02 15:04"),
							})
						case "status":
							if out.Message != "" {
								lastStatusMsg = out.Message
							}
						case "done":
							isDone = true
						}
					}

					if len(rowsToAdd) > 0 {
						const MaxResults = 5000
						currentLen := model.RowCount()
						if currentLen < MaxResults {
							if currentLen+len(rowsToAdd) > MaxResults {
								rowsToAdd = rowsToAdd[:MaxResults-currentLen]
								lastStatusMsg = fmt.Sprintf("结果过多，只显示前 %d 条", MaxResults)
							}
							model.AppendMany(rowsToAdd)
						}
					}

					// 更新状态栏
					if isDone {
						setStatus(fmt.Sprintf("Done. Matches: %d", model.RowCount()))
					} else if lastStatusMsg != "" {
						setStatus(lastStatusMsg)
					} else if len(rowsToAdd) > 0 {
						setStatus(fmt.Sprintf("Matches: %d", model.RowCount()))
					}
				})
			}
		}
	}()

	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		closeOnce.Do(func() {
			atomic.StoreUint32(&uiClosed, 1)
			close(closeCh)
		})
		debounceMu.Lock()
		if debounceT != nil {
			debounceT.Stop()
			debounceT = nil
		}
		debounceMu.Unlock()
		daemonMu.Lock()
		for _, d := range daemons {
			d.Close()
		}
		daemonMu.Unlock()
	})

	// 默认全盘（无弹窗）。
	if strings.TrimSpace(rootsEdit.Text()) == "" {
		rootsEdit.SetText(strings.Join(winutil.ListSearchableDrives(), ";"))
	}

	// 输入变化：立即清空旧结果，并取消旧查询；停止输入 400ms 后再开始新查询。
	onAnyQueryChanged := func() {
		debounceMu.Lock()
		if debounceT != nil {
			debounceT.Stop()
			debounceT = nil
		}
		gen++
		myGen := gen
		debounceMu.Unlock()

		model.Reset()
		clearSelection()
		forceTableRefresh()
		btnStop.SetEnabled(false)

		daemonMu.Lock()
		for _, d := range daemons {
			_ = d.SetQuery("", "", "", myGen, 30, 3)
		}
		daemonMu.Unlock()

		q1 := strings.TrimSpace(queryEdit.Text())
		q2 := strings.TrimSpace(query2Edit.Text())
		q3 := strings.TrimSpace(query3Edit.Text())
		if q1 == "" && q2 == "" && q3 == "" {
			setStatus("Ready")
			return
		}
		if (q1 != "" && !queryIsSearchable(q1)) || (q2 != "" && !queryIsSearchable(q2)) || (q3 != "" && !queryIsSearchable(q3)) {
			setStatus("输入太短：至少3个ASCII字符或2个Unicode字符才开始搜索")
			return
		}
		setStatus("输入中...停止输入后开始搜索")
		scheduleSearch()
	}

	queryEdit.TextChanged().Attach(onAnyQueryChanged)
	query2Edit.TextChanged().Attach(onAnyQueryChanged)
	query3Edit.TextChanged().Attach(onAnyQueryChanged)

	_ = mw.Run()
	return nil
}

func formatSize(s int64) string {
	const unit = 1024
	if s < unit {
		return fmt.Sprintf("%d B", s)
	}
	div, exp := int64(unit), 0
	for n := s / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(s)/float64(div), "KMGTPE"[exp])
}
