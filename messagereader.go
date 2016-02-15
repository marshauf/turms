package turms

import (
	"errors"
	"io"
)

var errNegativeRead = errors.New("bufio: reader returned negative count from Read")

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
		return 0, b.readErr()
	}
	if b.r == b.w {
		if b.err != nil {
			return 0, b.readErr()
		}
		b.fill() // buffer is empty
		if b.r == b.w {
			return 0, b.readErr()
		}
	}

	// copy as much as we can
	n = copy(p, b.buf[b.r:b.w])
	b.r += n
	return n, nil
}

func (b *messageReader) Reset() {
	b.r = 0
	b.w = 0
}

func (b *messageReader) ResetRead() {
	b.r = 0
}

// fill reads a new chunk into the buffer.
func (b *messageReader) fill() {
	if b.w >= len(b.buf) {
		panic("bufio: tried to fill full buffer")
	}

	// Read new data: try a limited number of times.
	for i := maxConsecutiveEmptyReads; i > 0; i-- {
		n, err := b.rd.Read(b.buf[b.w:])
		if n < 0 {
			panic(errNegativeRead)
		}
		b.w += n
		if err != nil {
			b.err = err
			return
		}
		if n > 0 {
			return
		}
	}
	b.err = io.ErrNoProgress
}

func (b *messageReader) readErr() error {
	err := b.err
	b.err = nil
	return err
}
