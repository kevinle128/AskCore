package jsonlstore

import (
	"bytes"
	"errors"
	"fmt"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
)

// scanResult is the committed part of a session file.
type scanResult struct {
	header  sessions.Header
	entries []sessions.Entry
	// valid is the size in bytes of the committed part. Bytes after it are a
	// torn or crashed tail.
	valid int
}

// scan reads a whole session file.
//
// A last line with no newline is a torn tail: the writer reports an append as
// stored only after the newline is written, so such a line was never reported.
// It is dropped even when it decodes. A bad line is corruption when a valid
// turn end follows it, because the log then went on after the damage. With no
// turn end after it the process died inside a turn, so the log is cut at the
// bad line. An unknown entry type is a newer format, never corruption.
func scan(data []byte) (scanResult, error) {
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return scanResult{}, fmt.Errorf("%w: line 1: header is incomplete", store.ErrSessionCorrupt)
	}
	header, err := sessions.DecodeHeader(data[:nl])
	if err != nil {
		if errors.Is(err, sessions.ErrFormatNewer) {
			return scanResult{}, fmt.Errorf("%w: %w", store.ErrSessionFormatNewer, err)
		}
		return scanResult{}, fmt.Errorf("%w: line 1: %w", store.ErrSessionCorrupt, err)
	}
	res := scanResult{header: header, valid: nl + 1}

	pos, lineNo, badLine := nl+1, 1, 0
	for pos < len(data) {
		lineNo++
		end := bytes.IndexByte(data[pos:], '\n')
		if end < 0 {
			break
		}
		next := pos + end + 1
		seq, entry, err := sessions.DecodeEntry(data[pos : pos+end])
		if errors.Is(err, sessions.ErrUnknownEntryType) {
			return scanResult{}, fmt.Errorf("%w: line %d: %w", store.ErrSessionFormatNewer, lineNo, err)
		}
		// A skipped line has no log position, so only a real entry is checked.
		ok := err == nil && (entry == nil || badLine != 0 || seq == len(res.entries))
		switch {
		case !ok:
			if badLine == 0 {
				badLine = lineNo
			}
		case badLine != 0:
			if _, closed := entry.(sessions.TurnClosed); closed {
				return scanResult{}, fmt.Errorf("%w: line %d is damaged and the log goes on after it", store.ErrSessionCorrupt, badLine)
			}
		default:
			if entry != nil {
				res.entries = append(res.entries, entry)
			}
			res.valid = next
		}
		pos = next
	}
	return res, nil
}
