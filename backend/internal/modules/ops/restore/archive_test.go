package restore

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixtureSQL(rows ...string) string {
	var b strings.Builder
	for _, t := range tables {
		fmt.Fprintf(&b, "COPY public.%s (id, details) FROM stdin;\n", t)
		for _, r := range rows {
			b.WriteString(r)
			b.WriteByte('\n')
		}
		b.WriteString("\\.\n")
	}
	b.WriteString("-- PostgreSQL database dump complete\n")
	return b.String()
}
func TestArchiveEvidence(t *testing.T) {
	long := strings.Repeat("z", 200000)
	a, e := inspectSQL(t.Context(), strings.NewReader(fixtureSQL(long, "2")))
	require.NoError(t, e)
	b, e := inspectSQL(t.Context(), strings.NewReader(fixtureSQL("2", long)))
	require.NoError(t, e)
	require.Equal(t, a, b)
	c, e := inspectSQL(t.Context(), strings.NewReader(fixtureSQL("2", long+"x")))
	require.NoError(t, e)
	require.NotEqual(t, a, c)
	require.Equal(t, "2", reportCounts(a)["api_keys"])
	for _, sql := range []string{"", fixtureSQL()[:20], strings.Replace(fixtureSQL(), "COPY public.users", "COPY public.absent", 1), strings.TrimSuffix(fixtureSQL(), "-- PostgreSQL database dump complete\n")} {
		_, e := inspectSQL(t.Context(), strings.NewReader(sql))
		require.ErrorIs(t, e, invalidArchive)
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, e = w.Write([]byte(fixtureSQL()))
	require.NoError(t, e)
	require.NoError(t, w.Close())
	data := buf.Bytes()
	data[len(data)-8] ^= 1
	_, _, e = inspectArchive(t.Context(), bytes.NewReader(data), &bytes.Buffer{})
	require.Error(t, e)
}
