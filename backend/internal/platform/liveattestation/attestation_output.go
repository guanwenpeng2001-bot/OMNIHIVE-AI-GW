package liveattestation

import "strings"

type attestationOutput struct {
	buffer   strings.Builder
	limit    int
	overflow bool
}

func (b *attestationOutput) Write(p []byte) (int, error) {
	n := min(len(p), b.limit-b.buffer.Len())
	_, _ = b.buffer.Write(p[:n])
	b.overflow = b.overflow || n < len(p)
	return len(p), nil
}

func (b *attestationOutput) String() string { return b.buffer.String() }
