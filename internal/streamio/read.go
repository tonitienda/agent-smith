package streamio

import "io"

// ReadAllLimit reads up to n bytes from r. A nil reader yields a nil body and
// nil error, matching callers that treat response-body reads as best effort.
func ReadAllLimit(r io.Reader, n int64) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(r, n))
}

// DrainClose drains up to n bytes from rc, then closes it. A nil closer is a
// no-op.
func DrainClose(rc io.ReadCloser, n int64) error {
	if rc == nil {
		return nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, n))
	return rc.Close()
}
