package dto

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type usageExportFailingWriter struct {
	bytes.Buffer
	remaining int
}

func (writer *usageExportFailingWriter) Write(data []byte) (int, error) {
	if len(data) > writer.remaining {
		written, _ := writer.Buffer.Write(data[:writer.remaining])
		writer.remaining = 0
		return written, io.ErrClosedPipe
	}
	writer.remaining -= len(data)
	return writer.Buffer.Write(data)
}
func TestUsageExport_PartialHeaderFailureTracksStarted(t *testing.T) {
	for _, remaining := range []int{0, 1, 3, 20} {
		writer := &usageExportFailingWriter{remaining: remaining}
		stream := NewUsageExportCSVStream(writer, []string{strings.Repeat("column", 1000)})
		require.ErrorIs(t, stream.WriteRows([][]string{{"row"}}), io.ErrClosedPipe)
		require.Equal(t, remaining > 0, stream.Started())
		require.Equal(t, remaining, writer.Len())
	}
}
func TestUsageExport_EmptyResultHeaderFailureUsesCorrectBoundary(t *testing.T) {
	for _, remaining := range []int{0, 1, 3, 5} {
		writer := &usageExportFailingWriter{remaining: remaining}
		stream := NewUsageExportCSVStream(writer, []string{"column"})
		require.ErrorIs(t, stream.Close(), io.ErrClosedPipe)
		require.Equal(t, remaining > 0, stream.Started())
	}
	var output bytes.Buffer
	stream := NewUsageExportCSVStream(&output, []string{"column"})
	require.NoError(t, stream.Close())
	require.True(t, stream.Started())
	require.NoError(t, stream.Close())
	require.Equal(t, "\uFEFFcolumn\n", output.String())
}
func TestUsageExport_CloseReportsBufferedFailureAfterWrite(t *testing.T) {
	writer := &usageExportFailingWriter{remaining: 100}
	stream := NewUsageExportCSVStream(writer, []string{"column"})
	require.NoError(t, stream.WriteRows([][]string{{"row"}}))
	// Put a final record in the CSV buffer to exercise Close's flush boundary.
	require.NoError(t, stream.writer.writer.Write([]string{"tail"}))
	writer.remaining = 0
	require.ErrorIs(t, stream.Close(), io.ErrClosedPipe)
	require.True(t, stream.Started())
}
