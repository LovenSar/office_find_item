//go:build windows

package app

import "github.com/lxn/walk"

type StringListModel struct {
	walk.ListModelBase
	items []string
}

func NewStringListModel() *StringListModel {
	return &StringListModel{items: make([]string, 0, 64)}
}

func (m *StringListModel) ItemCount() int {
	return len(m.items)
}

func (m *StringListModel) Value(index int) interface{} {
	if index < 0 || index >= len(m.items) {
		return ""
	}
	return m.items[index]
}

func (m *StringListModel) SetStrings(items []string) {
	m.items = append(m.items[:0], items...)
	m.PublishItemsReset()
}

func (m *StringListModel) Append(s string) {
	at := len(m.items)
	m.items = append(m.items, s)
	m.PublishItemsInserted(at, at)
}
