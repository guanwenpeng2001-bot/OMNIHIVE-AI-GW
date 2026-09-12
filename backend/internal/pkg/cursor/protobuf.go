package cursor

import (
	"encoding/binary"
	"fmt"
)

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

type message []byte

func (encoded *message) number(field uint64, value uint64) {
	*encoded = binary.AppendUvarint(*encoded, field<<3)
	*encoded = binary.AppendUvarint(*encoded, value)
}
func (encoded *message) bytes(field uint64, value []byte) {
	*encoded = binary.AppendUvarint(*encoded, field<<3|wireBytes)
	*encoded = binary.AppendUvarint(*encoded, uint64(len(value)))
	*encoded = append(*encoded, value...)
}
func (encoded *message) text(field uint64, value string) { encoded.bytes(field, []byte(value)) }

type fieldValue struct {
	number  uint64
	wire    uint64
	integer uint64
	data    []byte
}

func fields(data []byte) ([]fieldValue, error) {
	var decoded []fieldValue
	for len(data) > 0 {
		tag, consumed := binary.Uvarint(data)
		if consumed <= 0 || tag>>3 == 0 || tag>>3 > (1<<29)-1 {
			return nil, fmt.Errorf("cursor: malformed protobuf tag")
		}
		data = data[consumed:]
		field := fieldValue{number: tag >> 3, wire: tag & 7}
		switch field.wire {
		case wireVarint:
			field.integer, consumed = binary.Uvarint(data)
			if consumed <= 0 {
				return nil, fmt.Errorf("cursor: malformed protobuf varint")
			}
			data = data[consumed:]
		case wireBytes:
			length, consumed := binary.Uvarint(data)
			if consumed <= 0 {
				return nil, fmt.Errorf("cursor: malformed protobuf length")
			}
			data = data[consumed:]
			if length > uint64(len(data)) {
				return nil, fmt.Errorf("cursor: truncated protobuf bytes")
			}
			field.data, data = data[:int(length)], data[int(length):]
		case wireFixed32, wireFixed64:
			width := 4
			if field.wire == wireFixed64 {
				width = 8
			}
			if len(data) < width {
				return nil, fmt.Errorf("cursor: truncated protobuf fixed field")
			}
			if width == 4 {
				field.integer = uint64(binary.LittleEndian.Uint32(data))
			} else {
				field.integer = binary.LittleEndian.Uint64(data)
			}
			data = data[width:]
		default:
			return nil, fmt.Errorf("cursor: unsupported protobuf wire type %d", field.wire)
		}
		decoded = append(decoded, field)
	}
	return decoded, nil
}

func findField(decoded []fieldValue, number uint64) fieldValue {
	for _, field := range decoded {
		if field.number == number {
			return field
		}
	}
	return fieldValue{}
}
