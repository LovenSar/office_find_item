//go:build windows

package extract

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
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
		return nil, fmt.Errorf("UTF16PtrFromString failed: %v", err)
	}
	var out *iFilter
	hr, _, _ := procLoadIFilter.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&out)))
	if failed(uint32(hr)) || out == nil {
		hr32 := uint32(hr)
		// 常见IFilter相关错误代码
		switch hr32 {
		case 0x8004174B: // FILTER_E_EMBEDDING_UNAVAILABLE
			return nil, fmt.Errorf("LoadIFilter failed with FILTER_E_EMBEDDING_UNAVAILABLE (0x%08X): file may be corrupt or unsupported", hr32)
		case 0x8004170B: // FILTER_E_PASSWORD
			return nil, fmt.Errorf("LoadIFilter failed with FILTER_E_PASSWORD (0x%08X): file requires password", hr32)
		case 0x8004170C: // FILTER_E_UNKNOWNFORMAT
			return nil, fmt.Errorf("LoadIFilter failed with FILTER_E_UNKNOWNFORMAT (0x%08X): unknown file format", hr32)
		case 0x800401E3: // MK_E_UNAVAILABLE
			return nil, fmt.Errorf("LoadIFilter failed with MK_E_UNAVAILABLE (0x%08X): class not available, IFilter may not be registered", hr32)
		case 0x80070005: // E_ACCESSDENIED
			return nil, fmt.Errorf("LoadIFilter failed with E_ACCESSDENIED (0x%08X): access denied", hr32)
		case 0x80004005: // E_FAIL
			return nil, fmt.Errorf("LoadIFilter failed with E_FAIL (0x%08X): unspecified error", hr32)
		default:
			return nil, fmt.Errorf("LoadIFilter failed with HRESULT 0x%08X", hr32)
		}
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
	// 方法1：尝试通过注册表检查PDF IFilter是否已注册
	if hasPDFIFilterInRegistry() {
		if os.Getenv("OFIND_DEBUG") == "1" || os.Getenv("OFIND_DEBUG_CONSOLE") == "1" {
			log.Printf("[IFilter检测] 注册表显示PDF IFilter已注册")
		}
		// 注册表显示有IFilter，但还需要验证是否能实际加载
		// 继续执行方法2进行实际验证
	}

	// 方法2：尝试加载测试PDF文件
	f, err := os.CreateTemp("", "ofind_ifilter_*.pdf")
	if err != nil {
		return false
	}
	path := f.Name()
	// 创建一个更完整的PDF测试文件
	// 这是一个有效的PDF 1.1文件，包含简单的文本内容
	pdfContent := `%PDF-1.1
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj
2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj
3 0 obj
<<
/Type /Page
/Parent 2 0 R
/Resources <<
/Font <<
/F1 4 0 R
>>
>>
/Contents 5 0 R
>>
endobj
4 0 obj
<<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica
>>
endobj
5 0 obj
<<
/Length 44
>>
stream
BT
/F1 12 Tf
72 720 Td
(Test PDF for IFilter detection) Tj
ET
endstream
endobj
xref
0 6
0000000000 65535 f
0000000010 00000 n
0000000050 00000 n
0000000100 00000 n
0000000200 00000 n
0000000300 00000 n
trailer
<<
/Size 6
/Root 1 0 R
>>
startxref
400
%%EOF`

	_, _ = f.WriteString(pdfContent)
	_ = f.Close()
	defer os.Remove(path)

	if err := coInitialize(); err != nil {
		if os.Getenv("OFIND_DEBUG") == "1" || os.Getenv("OFIND_DEBUG_CONSOLE") == "1" {
			log.Printf("[IFilter检测] coInitialize failed: %v", err)
		}
		return false
	}
	defer coUninitialize()

	flt, err := loadIFilter(path)
	if err != nil {
		// 记录错误信息到调试日志（如果启用了调试）
		if os.Getenv("OFIND_DEBUG") == "1" || os.Getenv("OFIND_DEBUG_CONSOLE") == "1" {
			log.Printf("[IFilter检测] %v", err)
		}
		return false
	}
	if flt == nil {
		return false
	}
	flt.release()

	if os.Getenv("OFIND_DEBUG") == "1" || os.Getenv("OFIND_DEBUG_CONSOLE") == "1" {
		log.Printf("[IFilter检测] 成功加载PDF IFilter")
	}
	return true
}

// hasPDFIFilterInRegistry 检查注册表是否有PDF IFilter注册
func hasPDFIFilterInRegistry() bool {
	// 常见PDF IFilter的CLSID
	pdfIFilterCLSIDs := []string{
		"{E8978DA6-047F-4E3D-9C78-CDBE46041603}", // Adobe PDF IFilter
		"{3A2B4F1C-5C68-4EFA-AF0A-1F6EB7C6C2A5}", // Microsoft Office PDF IFilter
		"{A4773915-6EEF-4B05-99C2-003F9F7C6C2A}", // 另一个可能的PDF IFilter
		"{C9D1D0B7-6E8D-4C3D-8E8D-7C2F8F6E8D7C}", // Foxit PDF IFilter可能
	}

	// 检查HKCR\CLSID\{CLSID}是否存在
	for _, clsid := range pdfIFilterCLSIDs {
		keyPath := fmt.Sprintf(`SOFTWARE\Classes\CLSID\%s`, clsid)
		if registryKeyExists(keyPath) {
			if os.Getenv("OFIND_DEBUG") == "1" || os.Getenv("OFIND_DEBUG_CONSOLE") == "1" {
				log.Printf("[IFilter检测] 在注册表中找到PDF IFilter CLSID: %s", clsid)
			}
			return true
		}
	}

	// 检查HKLM\SOFTWARE\Classes\.pdf\PersistentHandler
	// PDF文件通常有特定的PersistentHandler
	phKeyPath := `SOFTWARE\Classes\.pdf\PersistentHandler`
	if registryKeyExists(phKeyPath) {
		if os.Getenv("OFIND_DEBUG") == "1" || os.Getenv("OFIND_DEBUG_CONSOLE") == "1" {
			log.Printf("[IFilter检测] 找到.pdf的PersistentHandler")
		}
		return true
	}

	return false
}

// registryKeyExists 检查注册表键是否存在
func registryKeyExists(keyPath string) bool {
	// 尝试打开注册表键
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.READ)
	if err == nil {
		k.Close()
		return true
	}

	// 也尝试当前用户
	k, err = registry.OpenKey(registry.CURRENT_USER, keyPath, registry.READ)
	if err == nil {
		k.Close()
		return true
	}

	// 也尝试HKCR（有些系统注册在HKCR）
	k, err = registry.OpenKey(registry.CLASSES_ROOT, keyPath, registry.READ)
	if err == nil {
		k.Close()
		return true
	}

	return false
}

// DiagnosePDFIFilter 返回详细的PDF IFilter诊断信息
func DiagnosePDFIFilter() string {
	var result strings.Builder

	result.WriteString("=== PDF IFilter 详细诊断 ===\n\n")

	// 1. 检查环境变量
	result.WriteString("1. 环境变量:\n")
	if debug := os.Getenv("OFIND_DEBUG"); debug != "" {
		result.WriteString(fmt.Sprintf("   OFIND_DEBUG=%s\n", debug))
	} else {
		result.WriteString("   OFIND_DEBUG 未设置\n")
	}
	if pureGo := os.Getenv("OFIND_PDF_PUREGO"); pureGo != "" {
		result.WriteString(fmt.Sprintf("   OFIND_PDF_PUREGO=%s\n", pureGo))
	} else {
		result.WriteString("   OFIND_PDF_PUREGO 未设置（将自动检测）\n")
	}
	result.WriteString("\n")

	// 2. 检查注册表
	result.WriteString("2. 注册表检查:\n")
	regFound := false

	// 常见PDF IFilter的CLSID
	pdfIFilterCLSIDs := []string{
		"{E8978DA6-047F-4E3D-9C78-CDBE46041603}", // Adobe PDF IFilter
		"{3A2B4F1C-5C68-4EFA-AF0A-1F6EB7C6C2A5}", // Microsoft Office PDF IFilter
		"{A4773915-6EEF-4B05-99C2-003F9F7C6C2A}", // 另一个可能的PDF IFilter
		"{C9D1D0B7-6E8D-4C3D-8E8D-7C2F8F6E8D7C}", // Foxit PDF IFilter可能
	}

	for _, clsid := range pdfIFilterCLSIDs {
		keyPath := fmt.Sprintf(`SOFTWARE\Classes\CLSID\%s`, clsid)
		if registryKeyExists(keyPath) {
			result.WriteString(fmt.Sprintf("   ✅ 找到PDF IFilter CLSID: %s\n", clsid))
			regFound = true
		}
	}

	// 检查HKLM\SOFTWARE\Classes\.pdf\PersistentHandler
	phKeyPath := `SOFTWARE\Classes\.pdf\PersistentHandler`
	if registryKeyExists(phKeyPath) {
		result.WriteString("   ✅ 找到.pdf的PersistentHandler\n")
		regFound = true
	}

	if !regFound {
		result.WriteString("   ❌ 未在注册表中找到PDF IFilter\n")
	}
	result.WriteString("\n")

	// 3. 实际测试IFilter加载
	result.WriteString("3. 实际IFilter加载测试:\n")

	// 创建临时PDF文件
	f, err := os.CreateTemp("", "diagnose_pdf_*.pdf")
	if err != nil {
		result.WriteString(fmt.Sprintf("   ❌ 创建临时文件失败: %v\n", err))
		return result.String()
	}
	path := f.Name()
	defer os.Remove(path)

	// 写入测试PDF内容
	pdfContent := `%PDF-1.1
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj
2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj
3 0 obj
<<
/Type /Page
/Parent 2 0 R
/Resources <<
/Font <<
/F1 4 0 R
>>
>>
/Contents 5 0 R
>>
endobj
4 0 obj
<<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica
>>
endobj
5 0 obj
<<
/Length 44
>>
stream
BT
/F1 12 Tf
72 720 Td
(Test PDF for IFilter detection) Tj
ET
endstream
endobj
xref
0 6
0000000000 65535 f
0000000010 00000 n
0000000050 00000 n
0000000100 00000 n
0000000200 00000 n
0000000300 00000 n
trailer
<<
/Size 6
/Root 1 0 R
>>
startxref
400
%%EOF`

	_, _ = f.WriteString(pdfContent)
	_ = f.Close()

	// 测试COM初始化
	if err := coInitialize(); err != nil {
		result.WriteString(fmt.Sprintf("   ❌ COM初始化失败: %v\n", err))
		return result.String()
	}
	defer coUninitialize()

	// 尝试加载IFilter
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		result.WriteString(fmt.Sprintf("   ❌ UTF16PtrFromString失败: %v\n", err))
		return result.String()
	}

	var out *iFilter
	hr, _, _ := procLoadIFilter.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&out)))
	if failed(uint32(hr)) || out == nil {
		hr32 := uint32(hr)
		// 常见IFilter相关错误代码
		switch hr32 {
		case 0x8004174B: // FILTER_E_EMBEDDING_UNAVAILABLE
			result.WriteString(fmt.Sprintf("   ❌ LoadIFilter失败: FILTER_E_EMBEDDING_UNAVAILABLE (0x%08X)\n", hr32))
			result.WriteString("       文件可能损坏或不支持\n")
		case 0x8004170B: // FILTER_E_PASSWORD
			result.WriteString(fmt.Sprintf("   ❌ LoadIFilter失败: FILTER_E_PASSWORD (0x%08X)\n", hr32))
			result.WriteString("       文件需要密码\n")
		case 0x8004170C: // FILTER_E_UNKNOWNFORMAT
			result.WriteString(fmt.Sprintf("   ❌ LoadIFilter失败: FILTER_E_UNKNOWNFORMAT (0x%08X)\n", hr32))
			result.WriteString("       未知文件格式\n")
		case 0x800401E3: // MK_E_UNAVAILABLE
			result.WriteString(fmt.Sprintf("   ❌ LoadIFilter失败: MK_E_UNAVAILABLE (0x%08X)\n", hr32))
			result.WriteString("       IFilter可能未注册\n")
		case 0x80070005: // E_ACCESSDENIED
			result.WriteString(fmt.Sprintf("   ❌ LoadIFilter失败: E_ACCESSDENIED (0x%08X)\n", hr32))
			result.WriteString("       访问被拒绝\n")
		case 0x80004005: // E_FAIL
			result.WriteString(fmt.Sprintf("   ❌ LoadIFilter失败: E_FAIL (0x%08X)\n", hr32))
			result.WriteString("       一般性失败（最常见原因：没有PDF IFilter）\n")
		default:
			result.WriteString(fmt.Sprintf("   ❌ LoadIFilter失败: HRESULT 0x%08X\n", hr32))
		}

		// 提供建议
		result.WriteString("\n   建议:\n")
		result.WriteString("   1. 安装PDF IFilter：Microsoft Office、Adobe Acrobat或WPS Office\n")
		result.WriteString("   2. 如果已安装，可能需要修复安装或重新注册IFilter\n")
		result.WriteString("   3. 使用纯Go PDF引擎：设置OFIND_PDF_PUREGO=1\n")
	} else {
		result.WriteString("   ✅ PDF IFilter加载成功\n")
		if out != nil {
			out.release()
		}
	}

	result.WriteString("\n=== 诊断完成 ===\n")
	return result.String()
}
