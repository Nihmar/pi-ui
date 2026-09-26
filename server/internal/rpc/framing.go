package rpc

import (
	"bytes"
	"io"
)

// Record framing constants. Either byte is a record terminator, not a separator: only
// line feed ends a record, carriage return is stripped when it precedes the line feed.
const (
	lineFeed     = '\n'
	carriageRet  = '\r'
	framingChunk = 64 << 10 // read granularity; records grow past it
)

// recordReader splits a byte stream into JSONL records.
//
// It reads bytes and looks for 0x0A: U+2028 (E2 80 A8) and U+2029 (E2 80 A9) are ordinary
// bytes inside a JSON string and never become record boundaries. A single trailing 0x0D
// is dropped, empty lines are skipped, and the last record of a stream without a
// terminator is returned before the end of the stream is reported. Returned records are
// copies: the caller owns them and the reader reuses its buffers.
type recordReader struct {
	src      io.Reader
	buf      []byte // received bytes, from consumed up to len(buf)
	consumed int    // offset of the first byte the caller has not seen yet
	scratch  []byte
	readErr  error // error that ended the stream, reported after the last record
}

// newRecordReader returns a reader over src. src is not closed by the reader.
func newRecordReader(src io.Reader) *recordReader {
	return &recordReader{src: src}
}

// Next returns the next record without its terminator, or the error that ended the stream
// (io.EOF for a clean end). A record without a trailing line feed is still returned once,
// with the stream error reported by the following call.
func (r *recordReader) Next() ([]byte, error) {
	for {
		if index := bytes.IndexByte(r.buf[r.consumed:], lineFeed); index >= 0 {
			line := r.buf[r.consumed : r.consumed+index]
			r.consumed += index + 1
			if record := r.take(line); record != nil {
				return record, nil
			}
			r.compact()
			continue
		}
		if r.readErr != nil {
			record := r.take(r.buf[r.consumed:])
			r.consumed = len(r.buf)
			if record != nil {
				return record, nil
			}
			return nil, r.readErr
		}
		if err := r.fill(); err != nil {
			r.readErr = err
		}
	}
}

// take copies a candidate record after trimming the terminator, or returns nil when the
// candidate is an empty line.
func (r *recordReader) take(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == carriageRet {
		line = line[:len(line)-1]
	}
	if len(line) == 0 {
		return nil
	}
	record := make([]byte, len(line))
	copy(record, line)
	return record
}

// fill appends the next chunk of the stream to the pending bytes.
func (r *recordReader) fill() error {
	r.compact()
	if r.scratch == nil {
		r.scratch = make([]byte, framingChunk)
	}
	n, err := r.src.Read(r.scratch)
	if n > 0 {
		// Plain append: a record larger than the read granularity grows the buffer
		// geometrically instead of copying the whole tail once per read.
		r.buf = append(r.buf, r.scratch[:n]...)
	}
	return err
}

// compact drops the bytes the caller already consumed, so a long-lived stream reuses one
// buffer instead of growing with every record.
func (r *recordReader) compact() {
	if r.consumed == 0 {
		return
	}
	if r.consumed == len(r.buf) {
		r.buf = r.buf[:0]
		r.consumed = 0
		return
	}
	r.buf = r.buf[:copy(r.buf, r.buf[r.consumed:])]
	r.consumed = 0
}
