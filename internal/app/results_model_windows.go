//go:build windows

package app

import "github.com/lxn/walk"

type ResultRow struct {
	Path      string
	Snippet   string
	Extension string
	Size      string
	ModTime   string
}

type ResultsModel struct {
	walk.TableModelBase
	rows []ResultRow
}

func NewResultsModel() *ResultsModel {
	return &ResultsModel{rows: make([]ResultRow, 0, 256)}
}

func (m *ResultsModel) RowCount() int {
	return len(m.rows)
}

func (m *ResultsModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.rows) {
		return ""
	}
	switch col {
	case 0:
		return row + 1
	case 1:
		return m.rows[row].Path
	case 2:
		return m.rows[row].Extension
	case 3:
		return m.rows[row].Snippet
	case 4:
		return m.rows[row].Size
	case 5:
		return m.rows[row].ModTime
	default:
		return ""
	}
}

func (m *ResultsModel) Reset() {
	// 直接丢弃旧 slice，避免某些情况下 TableView 仍残留旧行显示。
	m.rows = nil
	m.PublishRowsReset()
}

func (m *ResultsModel) Append(r ResultRow) {
	m.rows = append(m.rows, r)
	// 单行插入也强制 Reset，确保 UI 刷新
	m.PublishRowsReset()
}

func (m *ResultsModel) AppendMany(rs []ResultRow) {
	if len(rs) == 0 {
		return
	}
	// start := len(m.rows)
	m.rows = append(m.rows, rs...)
	// end := len(m.rows) - 1
	// m.PublishRowsInserted(start, end)
	
	// 强制全量刷新，解决部分系统下表格空白的问题
	m.PublishRowsReset()
}

func (m *ResultsModel) Row(row int) (ResultRow, bool) {
	if row < 0 || row >= len(m.rows) {
		return ResultRow{}, false
	}
	return m.rows[row], true
}
