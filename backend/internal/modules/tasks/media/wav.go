package media

import "encoding/binary"

const (
	MaxAudioBytes   = 32 << 20
	MaxAudioSeconds = 120
	MaxVideoBytes   = 256 << 20
)

// WAVDuration reads the actual PCM data chunk and validates its format's
// byte rate. Padding and optional RIFF chunks do not count towards duration.
func WAVDuration(b []byte) (int64, bool) {
	if len(b) < 44 || len(b) > MaxAudioBytes || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(b[4:8]))+8 != uint64(len(b)) {
		return 0, false
	}
	var bytesPerSecond, block, dataLen uint64
	for tail := b[12:]; len(tail) > 0; {
		if len(tail) < 8 {
			return 0, false
		}
		size := uint64(binary.LittleEndian.Uint32(tail[4:8]))
		padded := size + (size % 2)
		if padded > uint64(len(tail)-8) {
			return 0, false
		}
		value := tail[8 : 8+size]
		switch string(tail[:4]) {
		case "fmt ":
			if size < 16 || bytesPerSecond != 0 || binary.LittleEndian.Uint16(value) != 1 {
				return 0, false
			}
			channels := uint64(binary.LittleEndian.Uint16(value[2:4]))
			rate := uint64(binary.LittleEndian.Uint32(value[4:8]))
			bytesPerSecond = uint64(binary.LittleEndian.Uint32(value[8:12]))
			block = uint64(binary.LittleEndian.Uint16(value[12:14]))
			bits := uint64(binary.LittleEndian.Uint16(value[14:16]))
			if block == 0 || rate == 0 || bits == 0 || bits%8 != 0 || block != channels*(bits/8) || bytesPerSecond != rate*block {
				return 0, false
			}
		case "data":
			if dataLen != 0 || size == 0 {
				return 0, false
			}
			dataLen = size
		}
		tail = tail[8+padded:]
	}
	if bytesPerSecond == 0 || dataLen == 0 || dataLen%block != 0 || dataLen > MaxAudioSeconds*bytesPerSecond {
		return 0, false
	}
	return int64((dataLen*1000 + bytesPerSecond - 1) / bytesPerSecond), true
}
