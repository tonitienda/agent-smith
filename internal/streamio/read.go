package streamio

import "io"

// ReadAllLimit reads up to n bytes from r and returns them. It is the bounded
// counterpart to io.ReadAll, used to slurp error bodies without trusting a
// remote peer to keep them small. A short read is not an error; the returned
// error is whatever the underlying read surfaced (nil at a clean EOF).
func ReadAllLimit(r io.Reader, n int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, n))
}

// DrainClose discards up to n bytes from rc and then closes it, returning the
// close error. Draining a bounded prefix lets an HTTP transport reuse the
// connection without reading an unbounded body it does not care about.
func DrainClose(rc io.ReadCloser, n int64) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, n))
	return rc.Close()
}
