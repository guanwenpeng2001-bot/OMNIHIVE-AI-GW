package setup

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestGeneratedAdminPasswordFile(t *testing.T) {
	dir := t.TempDir()
	password, path, err := generateAdminPasswordFile(dir)
	require.NoError(t, err)
	require.NotEmpty(t, password)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, password+"\n", string(b))
	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), st.Mode().Perm())
	_, next, err := generateAdminPasswordFile(dir)
	require.NoError(t, err)
	require.NotEqual(t, path, next)
}
