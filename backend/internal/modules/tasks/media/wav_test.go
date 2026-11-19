package media

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func wav(seconds int) []byte {
	b := make([]byte, 44+seconds*16000)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 8000)
	binary.LittleEndian.PutUint32(b[28:], 16000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
	return b
}
func TestWAVDuration(t *testing.T) {
	b := wav(MaxAudioSeconds)
	ms, ok := WAVDuration(b)
	require.True(t, ok)
	require.EqualValues(t, 120000, ms)
	_, ok = WAVDuration(wav(MaxAudioSeconds + 1))
	require.False(t, ok)
	_, ok = WAVDuration(b[:44])
	require.False(t, ok)
	binary.LittleEndian.PutUint32(b[28:], 0xffffffff)
	_, ok = WAVDuration(b)
	require.False(t, ok)
}
