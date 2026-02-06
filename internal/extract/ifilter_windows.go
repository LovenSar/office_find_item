//go:build windows

package extract

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// 说明：这里使用 Windows 的 IFilter/LoadIFilter 来提取 doc/xls/ppt/pdf 等文本。
// 是否能成功取决于系统已安装的对应 IFilter（通常 Office/Adobe/系统组件提供）。

var (
	modOle32           = syscall.NewLazyDLL("ole32.dll")
	modQuery           = syscall.NewLazyDLL("query.dll")
	procCoInitializeEx = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize = modOle32.NewProc("CoUninitialize")
	procLoadIFilter    = modQuery.NewProc("LoadIFilter")
)

const (
	COINIT_APARTMENTTHREADED            = 0x2
	IFILTER_INIT_APPLY_INDEX_ATTRIBUTES = 0x4
	IFILTER_INIT_APPLY_OTHER_ATTRIBUTES = 0x8
	IFILTER_INIT_INDEXING_ONLY          = 0x40

	FILTER_E_END_OF_CHUNKS = 0x80041700
	FILTER_E_NO_MORE_TEXT  = 0x80041701

	CHUNK_TEXT = 0x1
)

type iUnknown struct {
	vtbl *iUnknownVTable
}

type iUnknownVTable struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type iFilter struct {
	vtbl *iFilterVTable
}

type iFilterVTable struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Init           uintptr
	GetChunk       uintptr
	GetText        uintptr
	GetValue       uintptr
	BindRegion     uintptr
}

type fullPropSpec struct {
	guidPropSet syscall.GUID
	propid      uint32
	kind        uint32
	lpwstr      *uint16
}

type statChunk struct {
	idChunk       uint32
	breakType     uint32
	flags         uint32
	locale        uint32
	attribute     fullPropSpec
	idChunkSource uint32
	startSource   uint32
	lenSource     uint32
}

func ifilterContains(ctx context.Context, path string, query string) (bool, error) {
	found, _, err := ifilterFindFirst(ctx, path, query, 0)
	return found, err
}

func ifilterFindFirst(ctx context.Context, path string, query string, contextLen int) (bool, string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return false, "", errors.New("query 为空")
	}

	// COM init per goroutine/thread: best-effort.
	if err := coInitialize(); err != nil {
		return false, "", err
	}
	defer coUninitialize()

	flt, err := loadIFilter(path)
	if err != nil {
		return false, "", err
	}
	defer flt.release()

	if err := flt.init(); err != nil {
		return false, "", err
	}

	for {
		if ctx.Err() != nil {
			return false, "", ctx.Err()
		}
		var chunk statChunk
		hr := flt.getChunk(&chunk)
		if hr == FILTER_E_END_OF_CHUNKS {
			return false, "", nil
		}
		if failed(hr) {
			// 某些 IFilter 会返回各种错误，按未命中处理
			return false, "", nil
		}
		if chunk.flags&CHUNK_TEXT == 0 {
			continue
		}
		for {
			if ctx.Err() != nil {
				return false, "", ctx.Err()
			}
			text, hr2 := flt.getText()
			if hr2 == FILTER_E_NO_MORE_TEXT {
				break
			}
			if failed(hr2) {
				break
			}
			if snips := FindSnippets(text, q, contextLen, 1); len(snips) > 0 {
				return true, snips[0], nil
			}
		}
	}
}

func ifilterFindSnippets(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("query 为空")
	}
	if maxSnippets <= 0 {
		maxSnippets = 1
	}

	// COM init per goroutine/thread: best-effort.
	if err := coInitialize(); err != nil {
		return nil, err
	}
	defer coUninitialize()

	flt, err := loadIFilter(path)
	if err != nil {
		return nil, err
	}
	defer flt.release()

	if err := flt.init(); err != nil {
		return nil, err
	}

	snips := make([]string, 0, maxSnippets)
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var chunk statChunk
		hr := flt.getChunk(&chunk)
		if hr == FILTER_E_END_OF_CHUNKS {
			return snips, nil
		}
		if failed(hr) {
			return snips, nil
		}
		if chunk.flags&CHUNK_TEXT == 0 {
			continue
		}
		for {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			text, hr2 := flt.getText()
			if hr2 == FILTER_E_NO_MORE_TEXT {
				break
			}
			if failed(hr2) {
				break
			}
			if text == "" {
				continue
			}
			found := FindSnippets(text, q, contextLen, maxSnippets-len(snips))
			if len(found) > 0 {
				snips = append(snips, found...)
				if len(snips) >= maxSnippets {
					return snips, nil
				}
			}
		}
	}
}

func coInitialize() error {
	hr, _, _ := procCoInitializeEx.Call(0, COINIT_APARTMENTTHREADED)
	if failed(uint32(hr)) {
		// RPC_E_CHANGED_MODE(0x80010106) 常见于已初始化为不同模型，允许继续
		if uint32(hr) == 0x80010106 {
			return nil
		}
		return syscall.Errno(hr)
	}
	return nil
}

func coUninitialize() {
	_, _, _ = procCoUninitialize.Call()
}

func loadIFilter(path string) (*iFilter, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var out *iFilter
	hr, _, _ := procLoadIFilter.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&out)))
	if failed(uint32(hr)) || out == nil {
		return nil, errors.New("LoadIFilter 失败：系统可能未安装对应 IFilter")
	}
	return out, nil
}

func (f *iFilter) release() {
	if f == nil || f.vtbl == nil {
		return
	}
	_, _, _ = syscall.SyscallN(f.vtbl.Release, uintptr(unsafe.Pointer(f)))
}

func (f *iFilter) init() error {
	// 参考: IFilter::Init
	flags := uint32(IFILTER_INIT_APPLY_INDEX_ATTRIBUTES | IFILTER_INIT_APPLY_OTHER_ATTRIBUTES | IFILTER_INIT_INDEXING_ONLY)
	var attrs uint32
	hr, _, _ := syscall.SyscallN(
		f.vtbl.Init,
		uintptr(unsafe.Pointer(f)),
		uintptr(flags),
		0,
		0,
		uintptr(unsafe.Pointer(&attrs)),
	)
	if failed(uint32(hr)) {
		return errors.New("IFilter Init 失败")
	}
	return nil
}

func (f *iFilter) getChunk(chunk *statChunk) uint32 {
	hr, _, _ := syscall.SyscallN(
		f.vtbl.GetChunk,
		uintptr(unsafe.Pointer(f)),
		uintptr(unsafe.Pointer(chunk)),
	)
	return uint32(hr)
}

func (f *iFilter) getText() (string, uint32) {
	// IFilter::GetText 会填充 WCHAR 缓冲区并返回长度
	buf := make([]uint16, 4096)
	size := uint32(len(buf))
	hr, _, _ := syscall.SyscallN(
		f.vtbl.GetText,
		uintptr(unsafe.Pointer(f)),
		uintptr(unsafe.Pointer(&size)),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if failed(uint32(hr)) {
		return "", uint32(hr)
	}
	if size == 0 {
		return "", uint32(hr)
	}
	return syscall.UTF16ToString(buf[:size]), uint32(hr)
}

func ifilterExtractText(ctx context.Context, path string, maxBytes int64) (string, error) {
	maxBytes = maxBytesOrDefault(maxBytes)

	if err := coInitialize(); err != nil {
		return "", err
	}
	defer coUninitialize()

	flt, err := loadIFilter(path)
	if err != nil {
		return "", err
	}
	defer flt.release()

	if err := flt.init(); err != nil {
		return "", err
	}

	var sb strings.Builder
	var approx int64

	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var chunk statChunk
		hr := flt.getChunk(&chunk)
		if hr == FILTER_E_END_OF_CHUNKS {
			break
		}
		if failed(hr) {
			break
		}
		if chunk.flags&CHUNK_TEXT == 0 {
			continue
		}
		for {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			text, hr2 := flt.getText()
			if hr2 == FILTER_E_NO_MORE_TEXT {
				break
			}
			if failed(hr2) {
				break
			}
			if text != "" {
				sb.WriteString(text)
				sb.WriteByte(' ')
				approx += int64(len(text)) + 1
				if approx >= maxBytes {
					return sb.String(), nil
				}
			}
		}
	}
	return sb.String(), nil
}

func failed(hr uint32) bool {
	return hr&0x80000000 != 0
}

// HasPDFIFilter best-effort checks whether the current Windows system has a usable
// PDF IFilter registered (e.g. via Office/WPS/PDF readers).
//
// This is intended for UI hints only; even if it returns true, a specific PDF may
// still fail to extract.
func HasPDFIFilter() bool {
	f, err := os.CreateTemp("", "ofind_ifilter_*.pdf")
	if err != nil {
		return false
	}
	path := f.Name()
	// Write minimal bytes so filters that peek at header don't immediately reject.
	_, _ = f.WriteString("%PDF-1.1\n%âãÏÓ\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n")
	_ = f.Close()
	defer os.Remove(path)

	if err := coInitialize(); err != nil {
		return false
	}
	defer coUninitialize()

	flt, err := loadIFilter(path)
	if err != nil || flt == nil {
		return false
	}
	flt.release()
	return true
}
