package service

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// boundedSSELineReader preserves ReadString's line endings, final unterminated
// line and read errors. It rejects oversized lines without returning a prefix.
// This branch has MaxLineSize but not upstream's event/queue limit settings;
// keep event parsing and stream lifetime in the existing protocol handlers.
type boundedSSELineReader struct {
	reader      *bufio.Reader
	maxLineSize int
}

func newBoundedSSELineReader(reader io.Reader, cfg *config.Config) *boundedSSELineReader {
	maxLineSize := defaultMaxLineSize
	if cfg != nil && cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = cfg.Gateway.MaxLineSize
	}
	return &boundedSSELineReader{
		reader:      bufio.NewReaderSize(reader, min(4096, maxLineSize)),
		maxLineSize: maxLineSize,
	}
}

func (r *boundedSSELineReader) ReadLineString() (string, error) {
	// Builder transfers the completed string without another full-line copy.
	var line strings.Builder
	for {
		part, err := r.reader.ReadSlice('\n')
		if len(part) > r.maxLineSize-line.Len() {
			return "", fmt.Errorf("%w: upstream SSE line exceeds %d bytes", bufio.ErrTooLong, r.maxLineSize)
		}
		_, _ = line.Write(part)
		if err == bufio.ErrBufferFull {
			continue
		}
		return line.String(), err
	}
}

func (s *AntigravityGatewayService) antigravitySSEConfig() *config.Config {
	if s == nil || s.settingService == nil {
		return nil
	}
	return s.settingService.cfg
}
