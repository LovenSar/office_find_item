//go:build windows

package app

import "github.com/lxn/walk"

type ResultRow struct {
	Path    string
	Snippet string
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
		return m.rows[row].Snippet
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
	at := len(m.rows)
	m.rows = append(m.rows, r)
	m.PublishRowsInserted(at, at)
}

func (m *ResultsModel) Row(row int) (ResultRow, bool) {
	if row < 0 || row >= len(m.rows) {
		return ResultRow{}, false
	}
	return m.rows[row], true
}
