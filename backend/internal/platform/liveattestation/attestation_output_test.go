package liveattestation

import (
	"io"
	"testing"
)

func TestAttestationOutputBoundsSubprocessBytes(t *testing.T) {
	b := &attestationOutput{limit: 4}
	if n, err := io.WriteString(b, "abcd"); n != 4 || err != nil || b.overflow {
		t.Fatal("exact limit", n, err)
	}
	if n, err := io.WriteString(b, "efgh"); n != 4 || err != nil || !b.overflow || b.String() != "abcd" {
		t.Fatal("overflow", n, err, b.String())
	}
}
