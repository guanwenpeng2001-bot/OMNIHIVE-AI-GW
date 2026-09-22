package cursor

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const maxFrameSize = 16 << 20

type frame struct {
	flags   byte
	payload []byte
}

func encodeFrame(payload []byte) []byte {
	encoded := make([]byte, 5, len(payload)+5)
	binary.BigEndian.PutUint32(encoded[1:], uint32(len(payload)))
	return append(encoded, payload...)
}

func readFrame(reader io.Reader) (frame, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return frame{}, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length > maxFrameSize || header[0]&^byte(3) != 0 {
		return frame{}, fmt.Errorf("cursor: invalid Connect envelope")
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return frame{}, err
	}
	if header[0]&1 != 0 {
		reader, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return frame{}, fmt.Errorf("cursor: invalid compressed frame")
		}
		defer func() { _ = reader.Close() }()
		payload, err = io.ReadAll(io.LimitReader(reader, maxFrameSize+1))
		if err != nil || len(payload) > maxFrameSize {
			return frame{}, fmt.Errorf("cursor: invalid expanded frame")
		}
	}
	return frame{flags: header[0], payload: payload}, nil
}

type ConnectError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (failure *ConnectError) Error() string { return "cursor Connect error: " + failure.Code }
func (failure *ConnectError) Authentication() bool {
	return failure.Code == "unauthenticated" || failure.Code == "permission_denied"
}

func trailerError(payload []byte) error {
	var trailer struct {
		Error *ConnectError `json:"error"`
	}
	if err := json.Unmarshal(payload, &trailer); err != nil {
		return fmt.Errorf("cursor: invalid Connect trailer")
	}
	if trailer.Error != nil {
		return trailer.Error
	}
	return nil
}
