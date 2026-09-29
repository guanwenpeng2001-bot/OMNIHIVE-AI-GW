package cursor

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

type failingHandshakeConnection struct {
	net.Conn
	closed   bool
	closeErr error
}

func (connection *failingHandshakeConnection) Read([]byte) (int, error) { return 0, io.EOF }
func (connection *failingHandshakeConnection) Write([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
func (connection *failingHandshakeConnection) Close() error {
	connection.closed = true
	return connection.closeErr
}

func TestNativeDialClosesConnectionOnFailure(t *testing.T) {
	for _, address := range []string{"invalid-address", "localhost:443"} {
		t.Run(address, func(t *testing.T) {
			closeErr := errors.New("connection close failed")
			local, peer := net.Pipe()
			defer func() { _ = local.Close(); _ = peer.Close() }()
			connection := &failingHandshakeConnection{Conn: local, closeErr: closeErr}
			client := newClientWithDial(func(context.Context, string, string) (net.Conn, error) { return connection, nil })
			transport, ok := client.HTTPClient.Transport.(*http2.Transport)
			require.True(t, ok)
			secured, err := transport.DialTLSContext(context.Background(), "tcp", address, &tls.Config{})
			require.Nil(t, secured)
			require.True(t, connection.closed)
			require.ErrorIs(t, err, closeErr)
			if address == "invalid-address" {
				var addressErr *net.AddrError
				require.ErrorAs(t, err, &addressErr)
			} else {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			}
		})
	}
}
