package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Body-decode errors, distinguishable so handlers keep their existing
// error semantics (400 "bad request" vs "cannot read body").
var (
	ErrBodyRead   = errors.New("read request body")
	ErrBodyDecode = errors.New("decode request body")
)

// parseBody reads and decodes the request body into T, returning the raw
// bytes as well so callers can preserve the verbatim JSON (QuerierListRequest
// keeps it in RawBody for cache matching).
//
// Semantics:
//   - io.ReadAll failure → error wrapping ErrBodyRead
//   - empty body / invalid JSON → error wrapping ErrBodyDecode
//     (an empty body must stay on the decode branch so handlers that answer
//     "bad request" don't drift to "cannot read body")
//
// json.Unmarshal triggers custom UnmarshalJSON implementations, so RawBody
// preservation is unaffected.
func parseBody[T any](r *http.Request) (*T, []byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrBodyRead, err)
	}
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, body, fmt.Errorf("%w: %v", ErrBodyDecode, err)
	}
	return &v, body, nil
}

// readRawBody reads the request body without decoding it, for handlers that
// pass the body through verbatim (flowlog detail, fast_list, dbdesc...).
func readRawBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBodyRead, err)
	}
	return body, nil
}

// decodeErrorMessage maps a parseBody failure to the handler's existing
// error message: read failures keep "cannot read body", decode failures
// keep "bad request" — so no wire-visible message drifts.
func decodeErrorMessage(err error) string {
	if errors.Is(err, ErrBodyRead) {
		return "cannot read body"
	}
	return "bad request"
}
