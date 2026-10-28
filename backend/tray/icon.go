package tray

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
)

// Identity names the application, including its single-instance lock.
const Identity = "{96B5A9E0-7F0D-4A67-BAC4-A7D887F23051}"

// Windows binds an unsigned notification GUID to the executable's full path.
// Keep it stable across updates in place, but give other folders their own GUID.
func identityForPath(path string) string {
	path = strings.ToLower(strings.ReplaceAll(filepath.Clean(path), "/", "\\"))
	sum := sha256.Sum256([]byte(Identity + "\x00" + path))
	sum[6] = sum[6]&0x0f | 0x80
	sum[8] = sum[8]&0x3f | 0x80
	return fmt.Sprintf("{%x-%x-%x-%x-%x}", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// iconImage extracts an ICO image close to the notification area's icon size.
// The complete ICO stays embedded in the EXE, so no companion file is needed.
func iconImage(ico []byte) ([]byte, error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico) != 0 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, fmt.Errorf("некорректный файл иконки трея")
	}
	count := int(binary.LittleEndian.Uint16(ico[4:]))
	if count == 0 || count > (len(ico)-6)/16 {
		return nil, fmt.Errorf("повреждён каталог иконки трея")
	}
	var image []byte
	best := 257
	for i := 0; i < count; i++ {
		entry := ico[6+i*16 : 6+(i+1)*16]
		size := int(entry[0])
		if size == 0 {
			size = 256
		}
		distance := size - 32
		if distance < 0 {
			distance = -distance
		}
		length := uint64(binary.LittleEndian.Uint32(entry[8:]))
		offset := uint64(binary.LittleEndian.Uint32(entry[12:]))
		if length == 0 || offset < uint64(6+count*16) || offset+length > uint64(len(ico)) {
			return nil, fmt.Errorf("повреждены данные иконки трея")
		}
		if distance < best {
			best, image = distance, ico[offset:offset+length]
		}
	}
	return image, nil
}
