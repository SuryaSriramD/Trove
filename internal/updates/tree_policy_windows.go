package updates

import (
	"encoding/binary"
	"errors"
	"golang.org/x/sys/windows"
	"io/fs"
	"os"
	"unicode/utf16"
)

// Windows chmod modes are not ACLs. Canonical logical modes make authenticated
// inventories portable across native copies; no POSIX ownership claim follows.
func inventoryMode(info fs.FileInfo) (uint32, error) {
	if info.IsDir() {
		return 0755, nil
	}
	if info.Mode().Perm()&0200 == 0 {
		return 0444, nil
	}
	return 0644, nil
}
func inventoryDirectoryPolicy(root *os.Root, name string, info fs.FileInfo) error {
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.IsDir() || !os.SameFile(info, opened) {
		return errors.New("application directory changed while inspecting")
	}
	return inventoryStreamPolicy(file, true)
}
func inventoryFilePolicy(file *os.File) error { return inventoryStreamPolicy(file, false) }
func inventoryStreamPolicy(file *os.File, directory bool) error {
	buffer := make([]byte, 64<<10)
	if err := windows.GetFileInformationByHandleEx(windows.Handle(file.Fd()), windows.FileStreamInfo, &buffer[0], uint32(len(buffer))); err != nil {
		if directory && errors.Is(err, windows.ERROR_HANDLE_EOF) {
			return nil
		}
		return errors.New("cannot verify Windows artifact stream inventory")
	}
	for offset, count := 0, 0; ; count++ {
		if count > 256 || offset < 0 || offset+24 > len(buffer) {
			return errors.New("invalid Windows artifact stream bounds")
		}
		next := int(binary.LittleEndian.Uint32(buffer[offset:]))
		length := int(binary.LittleEndian.Uint32(buffer[offset+4:]))
		// Directories can have no data streams. All other empty/partial results fail closed.
		if directory && count == 0 && next == 0 && length == 0 && binary.LittleEndian.Uint64(buffer[8:]) == 0 && binary.LittleEndian.Uint64(buffer[16:]) == 0 {
			return nil
		}
		if length <= 0 || length%2 != 0 || length > len(buffer)-offset-24 {
			return errors.New("invalid Windows artifact stream name")
		}
		units := make([]uint16, length/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(buffer[offset+24+2*i:])
		}
		if directory || string(utf16.Decode(units)) != "::$DATA" {
			return errors.New("named Windows streams require a separately authenticated copy policy; staging is refused")
		}
		if next == 0 {
			return nil
		}
		if next < 24+length {
			return errors.New("overlapping Windows artifact streams")
		}
		offset += next
	}
}
