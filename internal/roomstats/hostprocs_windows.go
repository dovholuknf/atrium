//go:build windows

package roomstats

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// HostProcs reads every process in one NtQuerySystemInformation call, which needs no admin and opens no process.
// The owner is left empty: OwnerOf reads it for the few that are flagged.
func HostProcs() ([]HostProc, error) {
	size := uint32(1 << 20)
	var buf []byte
	for {
		buf = make([]byte, size)
		var need uint32
		err := windows.NtQuerySystemInformation(windows.SystemProcessInformation, unsafe.Pointer(&buf[0]), size, &need)
		if err == nil {
			break
		}
		if err != windows.STATUS_INFO_LENGTH_MISMATCH || size >= 256<<20 {
			return nil, err
		}
		size = max(need+64<<10, size*2)
	}
	var out []HostProc
	for off := uint32(0); int(off) < len(buf); {
		e := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buf[off]))
		if e.UniqueProcessID != 0 {
			p := HostProc{PID: int(e.UniqueProcessID), PPID: int(e.InheritedFromUniqueProcessID),
				Name: e.ImageName.String(), CPU: time.Duration(e.UserTime+e.KernelTime) * 100,
				RSS: uint64(e.WorkingSetSize), IO: uint64(e.ReadTransferCount + e.WriteTransferCount), HasIO: true}
			if e.CreateTime > 0 {
				ft := windows.Filetime{LowDateTime: uint32(e.CreateTime), HighDateTime: uint32(e.CreateTime >> 32)}
				p.Started = time.Unix(0, ft.Nanoseconds())
			}
			out = append(out, p)
		}
		if e.NextEntryOffset == 0 {
			break
		}
		off += e.NextEntryOffset
	}
	return out, nil
}

// OwnerOf is the account a process runs as, "" when its token cannot be opened without admin.
func OwnerOf(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok) != nil {
		return ""
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return ""
	}
	acct, dom, _, err := u.User.Sid.LookupAccount("")
	if err != nil {
		return u.User.Sid.String()
	}
	if dom != "" {
		return dom + `\` + acct
	}
	return acct
}

// HostConns reads the TCP tables, which name every connection's owning process without admin.
func HostConns(pids []int) map[int]int {
	some := map[int]bool{}
	ok4 := tcpOwners(2, 24, 20, 0, some)
	ok6 := tcpOwners(23, 56, 52, 48, some)
	out := map[int]int{}
	for _, pid := range pids {
		switch {
		case some[pid]:
			out[pid] = ConnSome
		case ok4 && ok6:
			out[pid] = ConnNone
		default:
			out[pid] = ConnUnknown
		}
	}
	return out
}

// tcpOwners adds the owner of every row of one family's TCP_TABLE_OWNER_PID_ALL that is not listening.
func tcpOwners(family uint32, rowSize, pidAt, stateAt uintptr, into map[int]bool) bool {
	const tableOwnerPIDAll, listen = 5, 2
	size := uint32(64 << 10)
	for try := 0; try < 4; try++ {
		buf := make([]byte, size)
		r, _, _ := getExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0,
			uintptr(family), tableOwnerPIDAll, 0)
		if r == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			size += 16 << 10
			continue
		}
		if r != 0 {
			return false
		}
		n := uintptr(*(*uint32)(unsafe.Pointer(&buf[0])))
		for i := uintptr(0); i < n && 4+(i+1)*rowSize <= uintptr(len(buf)); i++ {
			row := 4 + i*rowSize
			state := *(*uint32)(unsafe.Pointer(&buf[row+stateAt]))
			if state != listen {
				into[int(*(*uint32)(unsafe.Pointer(&buf[row+pidAt])))] = true
			}
		}
		return true
	}
	return false
}
