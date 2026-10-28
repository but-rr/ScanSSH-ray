package tray

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestNotificationIdentityFollowsWindowsExecutablePath(t *testing.T) {
	first := identityForPath("C:/Apps/ScanSSH-ray.exe")
	if first != identityForPath("c:/apps/SCANSSH-RAY.EXE") {
		t.Fatal("Windows path casing changed the notification identity")
	}
	// Pin the persisted identity: changing this algorithm across releases would
	// leave registrations from an earlier release behind after a crash.
	if first != "{7fd1eaa5-33b9-8706-b638-b37335374078}" {
		t.Fatal("notification identity is incompatible with previous releases")
	}
	if first != identityForPath(`C:\Apps\ScanSSH-ray.exe`) {
		t.Fatal("Windows path separators changed the notification identity")
	}
	if first == identityForPath("C:/Other/ScanSSH-ray.exe") {
		t.Fatal("different folders must not reuse an unsigned executable's GUID")
	}
}

func TestIconRejectsCorruptInputs(t *testing.T) {
	for _, input := range [][]byte{nil, {0, 0, 1, 0, 0, 0}, {0, 0, 1, 0, 2, 0}, {0, 0, 2, 0, 0, 0}, make([]byte, 22)} {
		if _, err := iconImage(input); err == nil {
			t.Fatalf("accepted malformed ICO: %x", input)
		}
	}
	data := make([]byte, 22)
	binary.LittleEndian.PutUint16(data[2:], 1)
	binary.LittleEndian.PutUint16(data[4:], 1)
	binary.LittleEndian.PutUint32(data[14:], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(data[18:], 0xFFFFFFFF)
	if _, err := iconImage(data); err == nil {
		t.Fatal("accepted out-of-range ICO image")
	}
}

func TestIconChoosesSmallImageWithoutChangingSource(t *testing.T) {
	data := make([]byte, 6+2*16+8)
	binary.LittleEndian.PutUint16(data[2:], 1)
	binary.LittleEndian.PutUint16(data[4:], 2)
	for i, size := range []byte{128, 32} {
		offset := 6 + i*16
		data[offset], data[offset+1] = size, size
		binary.LittleEndian.PutUint32(data[offset+8:], 4)
		binary.LittleEndian.PutUint32(data[offset+12:], uint32(38+i*4))
	}
	copy(data[38:], []byte("big!tray"))
	copyBefore := append([]byte(nil), data...)
	image, err := iconImage(data)
	if err != nil || string(image) != "tray" {
		t.Fatalf("chosen image %q: %v", image, err)
	}
	if !bytes.Equal(copyBefore, data) {
		t.Fatal("modified embedded ICO")
	}
}
