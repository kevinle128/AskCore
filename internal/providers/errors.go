package providers

import "errors"

// ErrStreamIncomplete is the error of a stream that ended without a terminal
// event. The text keeps the words "ended without" so that text-based retry
// rules match it too.
var ErrStreamIncomplete = errors.New(incompleteMessage)

// ErrStreamClosed is returned by Assembler.Emit when the stream already has
// its result (a terminal event was applied or the request was cancelled).
var ErrStreamClosed = errors.New("stream is already settled")

// incompleteMessage is the errorMessage text of an incomplete stream.
const incompleteMessage = "stream ended without a terminal event"

// abortedMessage is the errorMessage text of a cancelled request.
const abortedMessage = "Request was aborted"
