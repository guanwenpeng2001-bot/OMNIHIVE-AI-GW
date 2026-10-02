package cursor

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
)

var (
	errMalformedMCPMetadataIdentity = errors.New("cursor: malformed MCP metadata identity")
	errMalformedMCPServerFilter     = errors.New("cursor: malformed MCP server filter")
	errMalformedMCPPrecheck         = errors.New("cursor: malformed MCP precheck")
	errMCPMetadataReply             = errors.New("cursor: MCP metadata reply failed")
)

// TransportError keeps typed causes for local admission and retry decisions.
// Its printable form never includes a URL, credential, or transport error text.
type TransportError struct{ cause error }

func (failure *TransportError) Error() string {
	return "cursor: Run transport failed: " + DiagnosticCode(failure.cause)
}
func (failure *TransportError) Unwrap() error { return failure.cause }

// SafeConnectCode accepts protocol codes, never arbitrary upstream strings.
func SafeConnectCode(code string) string {
	switch code {
	case "canceled", "unknown", "invalid_argument", "deadline_exceeded", "not_found",
		"already_exists", "permission_denied", "resource_exhausted", "failed_precondition",
		"aborted", "out_of_range", "unimplemented", "internal", "unavailable", "data_loss", "unauthenticated":
		return code
	default:
		return "unknown"
	}
}

// DiagnosticCode deliberately examines types and fixed sentinels only. In
// particular net/url errors and Connect messages can contain private context.
func DiagnosticCode(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	var connect *ConnectError
	if errors.As(err, &connect) {
		return "connect_" + SafeConnectCode(connect.Code)
	}
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		return "http_error"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns_error"
	}
	var certificate x509.UnknownAuthorityError
	var hostname x509.HostnameError
	if errors.As(err, &certificate) || errors.As(err, &hostname) {
		return "tls_verification_failed"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "network_timeout"
	}
	var operation *net.OpError
	if errors.As(err, &operation) {
		switch operation.Op {
		case "dial":
			return "dial_failed"
		case "read":
			return "read_failed"
		case "write":
			return "write_failed"
		}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "stream_truncated"
	}
	switch {
	case errors.Is(err, errMalformedMCPMetadataIdentity):
		return "malformed_mcp_metadata_identity"
	case errors.Is(err, errMalformedMCPServerFilter):
		return "malformed_mcp_server_filter"
	case errors.Is(err, errMalformedMCPPrecheck):
		return "malformed_mcp_precheck"
	case errors.Is(err, errMCPMetadataReply):
		return "mcp_metadata_reply_failed"
	}
	return "unclassified"
}
