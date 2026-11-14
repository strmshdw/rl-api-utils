package replay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf16"
)

// @perf-isolation(parity_test="tests/parity/replay_parser_parity_test.go")
// What: Bounded little-endian binary reader, UE3 FString decoder, and property payload skippers.
// Why: Eliminates per-primitive binary.Read reflection overhead and prevents panic/OOM allocations on truncated or malformed .replay buffers.
// How: Tracks stream offset against known endPos, validates all lengths against remaining() before allocating, and decodes primitives via a stack scratch buffer.
type boundedReader struct {
	r       io.ReadSeeker
	pos     int64
	endPos  int64
	scratch [8]byte
}

func newBoundedReader(r io.ReadSeeker) (*boundedReader, error) {
	if r == nil {
		return nil, errors.New("nil replay reader")
	}
	cur, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("seek current: %w", err)
	}
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("seek end: %w", err)
	}
	if _, err := r.Seek(cur, io.SeekStart); err != nil {
		return nil, fmt.Errorf("restore seek position: %w", err)
	}
	if end < cur {
		return nil, fmt.Errorf("invalid stream bounds: end %d < start %d", end, cur)
	}
	return &boundedReader{r: r, pos: cur, endPos: end}, nil
}

func (br *boundedReader) remaining() int64 {
	if br.pos >= br.endPos {
		return 0
	}
	return br.endPos - br.pos
}

func (br *boundedReader) readExact(dst []byte) error {
	need := int64(len(dst))
	if need == 0 {
		return nil
	}
	rem := br.remaining()
	if rem == 0 {
		return io.EOF
	}
	if need > rem {
		return fmt.Errorf("need %d bytes, %d remaining: %w", need, rem, io.ErrUnexpectedEOF)
	}
	n, err := io.ReadFull(br.r, dst)
	br.pos += int64(n)
	return err
}

func (br *boundedReader) readUint32() (uint32, error) {
	if err := br.readExact(br.scratch[:4]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(br.scratch[:4]), nil
}

func (br *boundedReader) readInt32() (int32, error) {
	u, err := br.readUint32()
	return int32(u), err
}

func (br *boundedReader) readInt64() (int64, error) {
	if err := br.readExact(br.scratch[:8]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(br.scratch[:8])), nil
}

func (br *boundedReader) readFloat32() (float32, error) {
	u, err := br.readUint32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(u), nil
}

func (br *boundedReader) readByte() (byte, error) {
	if err := br.readExact(br.scratch[:1]); err != nil {
		return 0, err
	}
	return br.scratch[0], nil
}

func (br *boundedReader) readBool() (bool, error) {
	b, err := br.readByte()
	return b != 0, err
}

func (br *boundedReader) readFString() (string, error) {
	length, err := br.readInt32()
	if err != nil {
		return "", err
	}
	if length == 0 {
		return "", nil
	}
	if length > maxFStringLen || length < -maxFStringLen {
		return "", fmt.Errorf("invalid fstring length %d (exceeds max %d)", length, maxFStringLen)
	}

	if length > 0 {
		byteLen := int64(length)
		if byteLen > br.remaining() {
			return "", fmt.Errorf("fstring length %d exceeds remaining %d: %w", length, br.remaining(), io.ErrUnexpectedEOF)
		}
		buf := make([]byte, int(length))
		if err := br.readExact(buf); err != nil {
			return "", err
		}
		return strings.TrimRight(string(buf), "\x00"), nil
	}

	charCount := int(-length)
	byteLen := int64(charCount) * 2
	if byteLen > br.remaining() {
		return "", fmt.Errorf("utf16 fstring bytes %d exceed remaining %d: %w", byteLen, br.remaining(), io.ErrUnexpectedEOF)
	}
	buf := make([]byte, int(byteLen))
	if err := br.readExact(buf); err != nil {
		return "", err
	}
	u16 := make([]uint16, charCount)
	for i := 0; i < charCount; i++ {
		u16[i] = binary.LittleEndian.Uint16(buf[i*2 : (i+1)*2])
	}
	n := 0
	for n < len(u16) && u16[n] != 0 {
		n++
	}
	return string(utf16.Decode(u16[:n])), nil
}

func (br *boundedReader) skip(n int64) error {
	if n == 0 {
		return nil
	}
	if n < 0 {
		return fmt.Errorf("negative skip size %d", n)
	}
	rem := br.remaining()
	if n > rem {
		return fmt.Errorf("skip %d exceeds remaining %d: %w", n, rem, io.ErrUnexpectedEOF)
	}
	next, err := br.r.Seek(n, io.SeekCurrent)
	if err != nil {
		return err
	}
	br.pos = next
	return nil
}

func (br *boundedReader) seekTo(absPos int64) error {
	if absPos < 0 || absPos > br.endPos {
		return fmt.Errorf("seek position %d out of bounds [0, %d]", absPos, br.endPos)
	}
	next, err := br.r.Seek(absPos, io.SeekStart)
	if err != nil {
		return err
	}
	br.pos = next
	return nil
}

func (br *boundedReader) withBound(size int64, fn func() error) error {
	if size < 0 {
		return fmt.Errorf("negative bounded payload size %d", size)
	}
	rem := br.remaining()
	if size > rem {
		return fmt.Errorf("payload size %d exceeds remaining %d: %w", size, rem, io.ErrUnexpectedEOF)
	}
	targetEnd := br.pos + size
	savedEnd := br.endPos
	br.endPos = targetEnd
	fnErr := fn()
	br.endPos = savedEnd
	if seekErr := br.seekTo(targetEnd); seekErr != nil {
		return seekErr
	}
	return fnErr
}

func (br *boundedReader) skipPropertyPayload(dataSize int64) error {
	if dataSize <= 0 {
		return nil
	}
	return br.skip(dataSize)
}

func (br *boundedReader) skipArrayPropertyPayload(dataSize int64) error {
	rem := dataSize - 4
	if rem <= 0 {
		return nil
	}
	return br.skip(rem)
}

func (br *boundedReader) readBytePropertyValue(dataSize int64) (string, int, error) {
	enumType, err := br.readFString()
	if err != nil {
		return "", 0, err
	}
	if dataSize <= 0 {
		return "", 0, nil
	}
	if enumType != "" && enumType != "None" {
		var enumVal string
		if err := br.withBound(dataSize, func() error {
			var readErr error
			enumVal, readErr = br.readFString()
			return readErr
		}); err != nil {
			return "", 0, err
		}
		return enumVal, 0, nil
	}
	if dataSize > maxBytePropDataSize {
		return "", 0, fmt.Errorf("invalid byte property dataSize %d", dataSize)
	}
	if dataSize > br.remaining() {
		return "", 0, fmt.Errorf("byte property dataSize %d exceeds remaining %d: %w", dataSize, br.remaining(), io.ErrUnexpectedEOF)
	}
	b, err := br.readByte()
	if err != nil {
		return "", 0, err
	}
	if dataSize > 1 {
		if err := br.skip(dataSize - 1); err != nil {
			return "", 0, err
		}
	}
	return "", int(b), nil
}
