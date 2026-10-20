//go:build windows && (amd64 || arm64)

package desktop

import (
	"syscall"
	"unsafe"
)

var (
	modDXGI                = syscall.NewLazyDLL("dxgi.dll")
	procCreateDXGIFactory1 = modDXGI.NewProc("CreateDXGIFactory1")

	// IID_IDXGIFactory1 from dxgi.h.
	iidIDXGIFactory1 = comGUID{
		Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba,
		Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87},
	}
)

const (
	// IDXGIFactory1 vtable: IUnknown (3), IDXGIObject (4), IDXGIFactory (5),
	// then EnumAdapters1.
	dxgiFactory1EnumAdapters1 = 12
	// IDXGIAdapter1 vtable: IUnknown (3), IDXGIObject (4), IDXGIAdapter (3),
	// then GetDesc1.
	dxgiAdapter1GetDesc1 = 10

	dxgiErrorNotFound       = 0x887A0002
	dxgiAdapterFlagSoftware = 0x2
)

// dxgiAdapterDesc1 mirrors DXGI_ADAPTER_DESC1.
type dxgiAdapterDesc1 struct {
	Description           [128]uint16
	VendorID              uint32
	DeviceID              uint32
	SubSysID              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	LUIDLow               uint32
	LUIDHigh              int32
	Flags                 uint32
}

// GPUAdapters lists the hardware DXGI adapters in DXGI order (the first is the
// system default). Software adapters such as the Microsoft Basic Render
// Driver are omitted. Use an entry's LUID in GPUOptions.AdapterLUID.
func GPUAdapters() ([]GPUAdapter, error) {
	if err := procCreateDXGIFactory1.Find(); err != nil {
		return nil, err
	}
	var factory unsafe.Pointer
	hr, _, _ := procCreateDXGIFactory1.Call(
		uintptr(unsafe.Pointer(&iidIDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if failedHRESULT(hr) {
		return nil, hresultError{Op: "CreateDXGIFactory1", Code: hr}
	}
	defer comRelease(factory)

	var adapters []GPUAdapter
	for index := uintptr(0); ; index++ {
		var adapter unsafe.Pointer
		hr, _, _ := syscall.SyscallN(comMethod(factory, dxgiFactory1EnumAdapters1),
			uintptr(factory), index, uintptr(unsafe.Pointer(&adapter)))
		if uint32(hr) == dxgiErrorNotFound {
			break
		}
		if failedHRESULT(hr) {
			return adapters, hresultError{Op: "IDXGIFactory1.EnumAdapters1", Code: hr}
		}
		var desc dxgiAdapterDesc1
		hr, _, _ = syscall.SyscallN(comMethod(adapter, dxgiAdapter1GetDesc1),
			uintptr(adapter), uintptr(unsafe.Pointer(&desc)))
		comRelease(adapter)
		if failedHRESULT(hr) {
			return adapters, hresultError{Op: "IDXGIAdapter1.GetDesc1", Code: hr}
		}
		if desc.Flags&dxgiAdapterFlagSoftware != 0 {
			continue
		}
		adapters = append(adapters, GPUAdapter{
			Name:                 syscall.UTF16ToString(desc.Description[:]),
			VendorID:             desc.VendorID,
			DeviceID:             desc.DeviceID,
			DedicatedVideoMemory: uint64(desc.DedicatedVideoMemory),
			LUID:                 GPUAdapterLUID{High: desc.LUIDHigh, Low: desc.LUIDLow},
		})
	}
	return adapters, nil
}
