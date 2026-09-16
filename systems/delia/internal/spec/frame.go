package spec

import (
	"encoding/binary"
	"errors"
	"io"
)

// MaxFrameLength bounds a single frame's payload (message_type byte plus
// fields) to guard ReadFrame against unbounded allocation from a corrupt or
// hostile length prefix. It is generous relative to any Tier-A message.
const MaxFrameLength = 64 << 20 // 64 MiB

// WriteFrame writes one off-chain message frame per the GAP-47/GAP-73
// layout: uint32 frame_length (big-endian, number of bytes that follow) ||
// uint8 message_type || payload. Raw bytes, never hex (DATA-018).
func WriteFrame(w io.Writer, msgType MessageType, payload []byte) error {
	length := uint64(1) + uint64(len(payload))
	if length > MaxFrameLength {
		return errors.New("spec: frame payload too large")
	}
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[0:4], uint32(length))
	header[4] = byte(msgType)
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads one off-chain message frame written by WriteFrame.
func ReadFrame(r io.Reader) (MessageType, []byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 {
		return 0, nil, errors.New("spec: frame_length must include the message_type byte")
	}
	if uint64(length) > MaxFrameLength {
		return 0, nil, errors.New("spec: frame_length exceeds MaxFrameLength")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return MessageType(body[0]), body[1:], nil
}
