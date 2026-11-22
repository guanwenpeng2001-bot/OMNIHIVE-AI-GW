package restore

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strconv"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

var tables = [...]string{"users", "api_keys", "billing_usage_entries", "upstream_jobs"}

const maxRestoreSQLBytes = 16 << 30

var errArchiveSize = &fault.Fault{
	Status: 422, Reason: "RESTORE_ARCHIVE_TOO_LARGE", Message: "The decompressed restore archive exceeds the size limit.",
}

func inspectArchive(ctx context.Context, raw io.Reader, sqlOut io.Writer) (string, map[string]string, error) {
	hash := sha256.New()
	gz, err := gzip.NewReader(io.TeeReader(raw, hash))
	if err != nil {
		return "", nil, invalidArchive
	}
	defer gz.Close()
	result, err := inspectArchiveSQL(ctx, gz, sqlOut, maxRestoreSQLBytes)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), result, err
}

func inspectArchiveSQL(ctx context.Context, src io.Reader, dst io.Writer, limit int64) (map[string]string, error) {
	bounded := &io.LimitedReader{R: src, N: limit}
	result, err := inspectSQL(ctx, io.TeeReader(bounded, dst))
	if bounded.N == 0 {
		var extra [1]byte
		if n, readErr := io.ReadFull(src, extra[:]); n != 0 {
			return nil, errArchiveSize
		} else if !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
	}
	return result, err
}

type copyEvidence struct {
	name string
	rows int64
	sum  [sha256.Size]byte
	row  hash.Hash
}

// pg_dump can visit equivalent rows in a different order on the replacement target.
func (e *copyEvidence) addRow() {
	digest := e.row.Sum(nil)
	carry := 0
	for i := len(digest) - 1; i >= 0; i-- {
		n := int(e.sum[i]) + int(digest[i]) + carry
		e.sum[i] = byte(n)
		carry = n >> 8
	}
	e.rows++
	e.row.Reset()
}

func inspectSQL(ctx context.Context, sql io.Reader) (map[string]string, error) {
	r := bufio.NewReaderSize(sql, 64*1024)
	evidence := make(map[string]string)
	var table *copyEvidence
	complete := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := r.ReadSlice('\n')
		if err == io.EOF && len(line) == 0 {
			break
		}
		if table != nil && err == nil && bytes.Equal(line, []byte("\\.\n")) {
			evidence[table.name] = strconv.FormatInt(table.rows, 10)
			evidence["hash/"+table.name] = hex.EncodeToString(table.sum[:])
			table = nil
			continue
		}
		if table != nil {
			_, _ = table.row.Write(line)
			for errors.Is(err, bufio.ErrBufferFull) {
				line, err = r.ReadSlice('\n')
				_, _ = table.row.Write(line)
			}
			if err != nil {
				return nil, invalidArchive
			}
			table.addRow()
			continue
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = r.ReadSlice('\n')
			}
			if err != nil {
				return nil, invalidArchive
			}
			continue
		}
		if err != nil {
			return nil, invalidArchive
		}
		if bytes.HasPrefix(line, []byte("COPY ")) && bytes.HasSuffix(line, []byte(" FROM stdin;\n")) {
			name, _, ok := strings.Cut(strings.TrimPrefix(string(line), "COPY "), " (")
			if !ok {
				return nil, invalidArchive
			}
			if _, exists := evidence[name]; exists {
				return nil, invalidArchive
			}
			table = &copyEvidence{name: name, row: sha256.New(), sum: sha256.Sum256(line)}
		}
		if bytes.Equal(line, []byte("-- PostgreSQL database dump complete\n")) {
			complete = true
		}
	}
	if !complete || table != nil {
		return nil, invalidArchive
	}
	for _, t := range tables {
		if _, ok := evidence["public."+t]; !ok {
			return nil, invalidArchive
		}
	}
	return evidence, nil
}

func reportCounts(evidence map[string]string) map[string]string {
	counts := make(map[string]string, len(tables))
	for _, t := range tables {
		if v, ok := evidence["public."+t]; ok {
			counts[t] = v
		}
	}
	return counts
}
