//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/go-ole/go-ole"
)

const (
	textPatternID              = 10014
	maxUIAParentDepth          = 16
	rpcChangedMode             = 0x80010106
	successAlreadyReady        = 0x00000001
	editMessageGetSelection    = 0x00B0
	windowMessageGetText       = 0x000D
	windowMessageGetTextLength = 0x000E
	sendMessageAbortIfHung     = 0x0002
	selectionMessageTimeoutMS  = 2000
	maxNativeEditCharacters    = 1024 * 1024
)

var (
	classUIAutomation     = ole.NewGUID("{ff48dba4-60ef-4201-aa87-54103eef594e}")
	interfaceUIAutomation = ole.NewGUID("{30cbe57d-d9d0-452a-ab13-7ac5ac4825ee}")
	interfaceTextPattern  = ole.NewGUID("{32eba289-3583-42c9-9c59-3b6d9a1e9b6a}")
)

type uiAutomation struct{ ole.IUnknown }

type uiAutomationVTable struct {
	ole.IUnknownVtbl
	CompareElements             uintptr
	CompareRuntimeIDs           uintptr
	GetRootElement              uintptr
	ElementFromHandle           uintptr
	ElementFromPoint            uintptr
	GetFocusedElement           uintptr
	GetRootElementBuildCache    uintptr
	ElementFromHandleBuildCache uintptr
	ElementFromPointBuildCache  uintptr
	GetFocusedElementBuildCache uintptr
	CreateTreeWalker            uintptr
	GetControlViewWalker        uintptr
	GetContentViewWalker        uintptr
	GetRawViewWalker            uintptr
}

func (automation *uiAutomation) vtable() *uiAutomationVTable {
	return (*uiAutomationVTable)(unsafe.Pointer(automation.RawVTable))
}

type uiAutomationElement struct{ ole.IUnknown }

type uiAutomationElementVTable struct {
	ole.IUnknownVtbl
	SetFocus                  uintptr
	GetRuntimeID              uintptr
	FindFirst                 uintptr
	FindAll                   uintptr
	FindFirstBuildCache       uintptr
	FindAllBuildCache         uintptr
	BuildUpdatedCache         uintptr
	GetCurrentPropertyValue   uintptr
	GetCurrentPropertyValueEx uintptr
	GetCachedPropertyValue    uintptr
	GetCachedPropertyValueEx  uintptr
	GetCurrentPatternAs       uintptr
}

func (element *uiAutomationElement) vtable() *uiAutomationElementVTable {
	return (*uiAutomationElementVTable)(unsafe.Pointer(element.RawVTable))
}

type uiAutomationTreeWalker struct{ ole.IUnknown }

type uiAutomationTreeWalkerVTable struct {
	ole.IUnknownVtbl
	GetParentElement uintptr
}

func (walker *uiAutomationTreeWalker) vtable() *uiAutomationTreeWalkerVTable {
	return (*uiAutomationTreeWalkerVTable)(unsafe.Pointer(walker.RawVTable))
}

type uiAutomationTextPattern struct{ ole.IUnknown }

type uiAutomationTextPatternVTable struct {
	ole.IUnknownVtbl
	RangeFromPoint uintptr
	RangeFromChild uintptr
	GetSelection   uintptr
}

func (pattern *uiAutomationTextPattern) vtable() *uiAutomationTextPatternVTable {
	return (*uiAutomationTextPatternVTable)(unsafe.Pointer(pattern.RawVTable))
}

type uiAutomationTextRangeArray struct{ ole.IUnknown }

type uiAutomationTextRangeArrayVTable struct {
	ole.IUnknownVtbl
	GetLength  uintptr
	GetElement uintptr
}

func (array *uiAutomationTextRangeArray) vtable() *uiAutomationTextRangeArrayVTable {
	return (*uiAutomationTextRangeArrayVTable)(unsafe.Pointer(array.RawVTable))
}

type uiAutomationTextRange struct{ ole.IUnknown }

type guiThreadInfo struct {
	Size      uint32
	Flags     uint32
	Active    uintptr
	Focus     uintptr
	Capture   uintptr
	MenuOwner uintptr
	MoveSize  uintptr
	Caret     uintptr
	CaretRect rectangle
}

type uiAutomationTextRangeVTable struct {
	ole.IUnknownVtbl
	Clone                 uintptr
	Compare               uintptr
	CompareEndpoints      uintptr
	ExpandToEnclosingUnit uintptr
	FindAttribute         uintptr
	FindText              uintptr
	GetAttributeValue     uintptr
	GetBoundingRectangles uintptr
	GetEnclosingElement   uintptr
	GetText               uintptr
}

func (textRange *uiAutomationTextRange) vtable() *uiAutomationTextRangeVTable {
	return (*uiAutomationTextRangeVTable)(unsafe.Pointer(textRange.RawVTable))
}

func (d *windowsDesktop) SelectedText(ctx context.Context, maxLength int) (string, error) {
	if maxLength <= 0 {
		return "", errors.New("选中文本长度上限必须大于 0")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	shouldUninitialize, err := initializeCOM()
	if err != nil {
		return "", fmt.Errorf("初始化 UI Automation 失败: %w", err)
	}
	if shouldUninitialize {
		defer ole.CoUninitialize()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	unknown, err := ole.CreateInstance(classUIAutomation, interfaceUIAutomation)
	if err != nil {
		return "", fmt.Errorf("创建 UI Automation 客户端失败: %w", err)
	}
	automation := (*uiAutomation)(unsafe.Pointer(unknown))
	defer automation.Release()

	focused, err := automation.focusedElement()
	if err != nil {
		return "", fmt.Errorf("读取当前焦点控件失败: %w", err)
	}
	walker, err := automation.rawViewWalker()
	if err != nil {
		focused.Release()
		return "", fmt.Errorf("创建 UI Automation 导航器失败: %w", err)
	}
	defer walker.Release()

	current := focused
	foundTextPattern := false
	for depth := 0; current != nil && depth < maxUIAParentDepth; depth++ {
		if err := ctx.Err(); err != nil {
			current.Release()
			return "", err
		}
		text, supported, readErr := current.selectedText(maxLength)
		foundTextPattern = foundTextPattern || supported
		if readErr == nil && strings.TrimSpace(text) != "" {
			current.Release()
			return text, nil
		}

		parent, parentErr := walker.parent(current)
		current.Release()
		current = nil
		if parentErr != nil || parent == nil {
			break
		}
		current = parent
	}
	if current != nil {
		current.Release()
	}
	if text, nativeErr := selectedNativeEditText(ctx, maxLength); nativeErr == nil {
		return text, nil
	} else if errors.Is(nativeErr, ErrNoSelectedText) || errors.Is(nativeErr, ErrSelectedTextTooLong) {
		return "", nativeErr
	} else if !errors.Is(nativeErr, ErrSelectionUnsupported) {
		return "", fmt.Errorf("读取原生文本选区失败: %w", nativeErr)
	}
	if foundTextPattern {
		return "", ErrNoSelectedText
	}
	return "", ErrSelectionUnsupported
}

func selectedNativeEditText(ctx context.Context, maxLength int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	foreground, _, _ := procGetForegroundWindow.Call()
	if foreground == 0 {
		return "", ErrSelectionUnsupported
	}
	threadID, _, _ := procGetWindowThreadProcessID.Call(foreground, 0)
	if threadID == 0 {
		return "", ErrSelectionUnsupported
	}
	info := guiThreadInfo{Size: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	result, _, _ := procGetGUIThreadInfo.Call(threadID, uintptr(unsafe.Pointer(&info)))
	if result == 0 || info.Focus == 0 {
		return "", ErrSelectionUnsupported
	}

	classBuffer := make([]uint16, 128)
	length, _, _ := procGetClassNameW.Call(
		info.Focus,
		uintptr(unsafe.Pointer(&classBuffer[0])),
		uintptr(len(classBuffer)),
	)
	if length == 0 {
		return "", ErrSelectionUnsupported
	}
	className := strings.ToUpper(string(utf16.Decode(classBuffer[:length])))
	if className != "EDIT" && !strings.HasPrefix(className, "RICHEDIT") {
		return "", ErrSelectionUnsupported
	}

	var start uint32
	var end uint32
	if _, err := sendMessageTimeout(
		info.Focus,
		editMessageGetSelection,
		uintptr(unsafe.Pointer(&start)),
		uintptr(unsafe.Pointer(&end)),
	); err != nil {
		return "", err
	}
	if end <= start {
		return "", ErrNoSelectedText
	}
	if end-start > uint32(maxLength) {
		return "", ErrSelectedTextTooLong
	}

	textLength, err := sendMessageTimeout(info.Focus, windowMessageGetTextLength, 0, 0)
	if err != nil {
		return "", err
	}
	if textLength <= 0 || textLength > maxNativeEditCharacters || uint64(end) > uint64(textLength) {
		return "", ErrSelectionUnsupported
	}
	buffer := make([]uint16, textLength+1)
	if _, err := sendMessageTimeout(
		info.Focus,
		windowMessageGetText,
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&buffer[0])),
	); err != nil {
		return "", err
	}
	return string(utf16.Decode(buffer[start:end])), nil
}

func sendMessageTimeout(window uintptr, message uint32, wParam uintptr, lParam uintptr) (uintptr, error) {
	var messageResult uintptr
	result, _, callErr := procSendMessageTimeoutW.Call(
		window,
		uintptr(message),
		wParam,
		lParam,
		sendMessageAbortIfHung,
		selectionMessageTimeoutMS,
		uintptr(unsafe.Pointer(&messageResult)),
	)
	if result == 0 {
		return 0, fmt.Errorf("读取原生文本控件超时或失败: %w", callErr)
	}
	return messageResult, nil
}

func initializeCOM() (bool, error) {
	err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if err == nil {
		return true, nil
	}
	var oleErr *ole.OleError
	if !errors.As(err, &oleErr) {
		return false, err
	}
	switch uint32(oleErr.Code()) {
	case successAlreadyReady:
		return true, nil
	case rpcChangedMode:
		return false, nil
	default:
		return false, err
	}
}

func (automation *uiAutomation) focusedElement() (*uiAutomationElement, error) {
	var element *uiAutomationElement
	hresult, _, _ := syscall.SyscallN(
		automation.vtable().GetFocusedElement,
		uintptr(unsafe.Pointer(automation)),
		uintptr(unsafe.Pointer(&element)),
	)
	if hresult != 0 {
		return nil, ole.NewError(hresult)
	}
	if element == nil {
		return nil, ErrSelectionUnsupported
	}
	return element, nil
}

func (automation *uiAutomation) rawViewWalker() (*uiAutomationTreeWalker, error) {
	var walker *uiAutomationTreeWalker
	hresult, _, _ := syscall.SyscallN(
		automation.vtable().GetRawViewWalker,
		uintptr(unsafe.Pointer(automation)),
		uintptr(unsafe.Pointer(&walker)),
	)
	if hresult != 0 {
		return nil, ole.NewError(hresult)
	}
	if walker == nil {
		return nil, ErrSelectionUnsupported
	}
	return walker, nil
}

func (walker *uiAutomationTreeWalker) parent(element *uiAutomationElement) (*uiAutomationElement, error) {
	var parent *uiAutomationElement
	hresult, _, _ := syscall.SyscallN(
		walker.vtable().GetParentElement,
		uintptr(unsafe.Pointer(walker)),
		uintptr(unsafe.Pointer(element)),
		uintptr(unsafe.Pointer(&parent)),
	)
	if hresult != 0 {
		return nil, ole.NewError(hresult)
	}
	return parent, nil
}

func (element *uiAutomationElement) selectedText(maxLength int) (string, bool, error) {
	var unknown *ole.IUnknown
	hresult, _, _ := syscall.SyscallN(
		element.vtable().GetCurrentPatternAs,
		uintptr(unsafe.Pointer(element)),
		textPatternID,
		uintptr(unsafe.Pointer(interfaceTextPattern)),
		uintptr(unsafe.Pointer(&unknown)),
	)
	if hresult != 0 || unknown == nil {
		return "", false, nil
	}
	pattern := (*uiAutomationTextPattern)(unsafe.Pointer(unknown))
	defer pattern.Release()

	var ranges *uiAutomationTextRangeArray
	hresult, _, _ = syscall.SyscallN(
		pattern.vtable().GetSelection,
		uintptr(unsafe.Pointer(pattern)),
		uintptr(unsafe.Pointer(&ranges)),
	)
	if hresult != 0 {
		return "", true, ole.NewError(hresult)
	}
	if ranges == nil {
		return "", true, nil
	}
	defer ranges.Release()

	length, err := ranges.length()
	if err != nil {
		return "", true, err
	}
	parts := make([]string, 0, length)
	remaining := maxLength*2 + 1
	for index := int32(0); index < length && remaining > 0; index++ {
		textRange, rangeErr := ranges.element(index)
		if rangeErr != nil {
			return "", true, rangeErr
		}
		text, textErr := textRange.text(int32(remaining))
		textRange.Release()
		if textErr != nil {
			return "", true, textErr
		}
		if text != "" {
			parts = append(parts, text)
			remaining -= len([]rune(text))
		}
	}
	return strings.Join(parts, "\n"), true, nil
}

func (array *uiAutomationTextRangeArray) length() (int32, error) {
	var length int32
	hresult, _, _ := syscall.SyscallN(
		array.vtable().GetLength,
		uintptr(unsafe.Pointer(array)),
		uintptr(unsafe.Pointer(&length)),
	)
	if hresult != 0 {
		return 0, ole.NewError(hresult)
	}
	return length, nil
}

func (array *uiAutomationTextRangeArray) element(index int32) (*uiAutomationTextRange, error) {
	var textRange *uiAutomationTextRange
	hresult, _, _ := syscall.SyscallN(
		array.vtable().GetElement,
		uintptr(unsafe.Pointer(array)),
		uintptr(index),
		uintptr(unsafe.Pointer(&textRange)),
	)
	if hresult != 0 {
		return nil, ole.NewError(hresult)
	}
	if textRange == nil {
		return nil, ErrNoSelectedText
	}
	return textRange, nil
}

func (textRange *uiAutomationTextRange) text(maxLength int32) (string, error) {
	var value *uint16
	hresult, _, _ := syscall.SyscallN(
		textRange.vtable().GetText,
		uintptr(unsafe.Pointer(textRange)),
		uintptr(maxLength),
		uintptr(unsafe.Pointer(&value)),
	)
	if hresult != 0 {
		return "", ole.NewError(hresult)
	}
	if value == nil {
		return "", nil
	}
	defer ole.SysFreeString((*int16)(unsafe.Pointer(value)))
	return ole.BstrToString(value), nil
}
