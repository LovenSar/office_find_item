//go:build windows

package app

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"office_find_item/internal/winutil"
)

func RunUI() error {
	// 兼容性：某些系统/环境下 tooltip 相关 common controls 未初始化会导致 TTM_ADDTOOL failed。
	// walk 默认 init 的 ICC 集合未包含 ICC_WIN95_CLASSES/ICC_BAR_CLASSES，这里补齐。
	walk.AppendToWalkInit(func() {
		var icc win.INITCOMMONCONTROLSEX
		icc.DwSize = uint32(unsafe.Sizeof(icc))
		icc.DwICC = win.ICC_WIN95_CLASSES | win.ICC_BAR_CLASSES
		win.InitCommonControlsEx(&icc)
	})

	var (
		mw          *walk.MainWindow
		rootsEdit   *walk.LineEdit
		queryEdit   *walk.LineEdit
		query2Edit  *walk.LineEdit
		query3Edit  *walk.LineEdit
		status      *walk.Label
		btnStop     *walk.PushButton
		tableView   *walk.TableView

		daemonMu   sync.Mutex
		daemons    map[string]*daemonProcess
		rootsKey   string
		debounceMu sync.Mutex
		debounceT  *time.Timer
		gen        uint64

		model = NewResultsModel()
	)

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
		status.SetText("已取消，等待输入...")
		btnStop.SetEnabled(false)
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
			status.SetText("Ready")
			return
		}
		roots := strings.TrimSpace(rootsEdit.Text())
		if roots == "" {
			status.SetText("请先选择目录或全盘")
			return
		}

		// cancel previous query (but keep daemons)
		model.Reset()
		clearSelection()
		forceTableRefresh()
		btnStop.SetEnabled(true)
		status.SetText("Searching...")

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

		daemonMu.Lock()
		if daemons == nil {
			daemons = map[string]*daemonProcess{}
		}
		if rootsKey != nextKey {
			for _, d := range daemons {
				d.Close()
			}
			daemons = map[string]*daemonProcess{}
			rootsKey = nextKey
		}
		for _, root := range parts {
			if root == "" {
				continue
			}
			if _, ok := daemons[root]; ok {
				continue
			}
			dproc, err := startDaemonProcess(exePath, root, 0, func(out daemonOut) {
				mw.Synchronize(func() {
					debounceMu.Lock()
					curGen := gen
					debounceMu.Unlock()
					if out.QueryID != curGen {
						return
					}
					switch out.Type {
					case "result":
						snip := strings.Join(out.Snippets, "  |  ")
						model.Append(ResultRow{Path: out.Path, Snippet: snip})
						status.SetText(fmt.Sprintf("Matches: %d", model.RowCount()))
					case "done":
						status.SetText(fmt.Sprintf("Done. Matches: %d", model.RowCount()))
					case "status":
						if out.Message != "" {
							status.SetText(out.Message)
						}
					}
				})
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
								status.SetText("无法获取桌面目录")
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
					declarative.PushButton{AssignTo: &btnStop, Text: "停止", Enabled: false, OnClicked: stopSearch, ColumnSpan: 6},
				},
			},
			declarative.Label{AssignTo: &status, Text: "Ready"},
			declarative.TableView{
				AssignTo: &tableView,
				Model:    model,
				LastColumnStretched: true,
				ColumnsSizable:       true,
				Columns: []declarative.TableViewColumn{
					{Title: "#", Width: 44},
					{Title: "Path", Width: 520},
					{Title: "Context (±30)", Width: 360},
				},
				OnItemActivated: revealSelected,
			},
		},
	}

	if err := mwDecl.Create(); err != nil {
		return err
	}

	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
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
			status.SetText("Ready")
			return
		}
		status.SetText("输入中...停止输入后开始搜索")
		scheduleSearch()
	}

	queryEdit.TextChanged().Attach(onAnyQueryChanged)
	query2Edit.TextChanged().Attach(onAnyQueryChanged)
	query3Edit.TextChanged().Attach(onAnyQueryChanged)

	_ = mw.Run()
	return nil
}
