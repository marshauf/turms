package turms

import (
	"errors"
	"io"
)

var errNegativeRead = errors.New("bufio: reader returned negative count from Read")
var ErrBufferFull = errors.New("bufio: full buffer")

const minReadBufferSize = 16
const maxConsecutiveEmptyReads = 100

type messageReader struct {
	buf  []byte
	rd   io.Reader // reader provided by the client
	r, w int       // buf read and write positions
	err  error
}

func newMessageReader(rd io.Reader, size int) *messageReader {
	if size < minReadBufferSize {
		size = minReadBufferSize
	}
	return &messageReader{
		buf: make([]byte, size),
		rd:  rd,
	}
}

// Read reads data into p.
// It returns the number of bytes read into p.
// The bytes are taken from at most one Read on the underlying Reader,
// hence n may be less than len(p).
// At EOF, the count will be zero and err will be io.EOF.
func (b *messageReader) Read(p []byte) (n int, err error) {
	n = len(p)
	if n == 0 {
		return 0, nil
	}
	if b.r == b.w {
		if err := b.fill(); err != nil {
			return 0, err
		}
	}

	// copy as much as we can
	n = copy(p, b.buf[b.r:b.w])
	b.r += n
	return n, nil
}

// Reset discards any buffered data, and resets all state.
func (b *messageReader) Reset() {
	b.r = 0
	b.w = 0
}

// ResetRead sets the read position to the beginning of the buffer.
func (b *messageReader) ResetRead() {
	b.r = 0
}

// fill reads a new chunk into the buffer.
func (b *messageReader) fill() error {
	if b.w >= len(b.buf) {
		return ErrBufferFull
	}

	// Read new data: try a limited number of times.
	for i := maxConsecutiveEmptyReads; i > 0; i-- {
		n, err := b.rd.Read(b.buf[b.w:])
		if n < 0 {
			panic(errNegativeRead)
		}
		b.w += n
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
	}
	return io.ErrNoProgress
}
