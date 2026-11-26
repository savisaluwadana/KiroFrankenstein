package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// MaxBodyBytes is the request body cap (security steering, Req 26.5).
const MaxBodyBytes = 256 << 10

// decodeBody strictly decodes the JSON request body into dst: unknown fields
// and trailing data are rejected (400) and a body over MaxBodyBytes gives 413.
// An empty body is allowed only when optional is true, and then dst keeps its
// zero value.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, optional bool) error {
	if r.ContentLength > MaxBodyBytes {
		return problemFor(&http.MaxBytesError{Limit: MaxBodyBytes})
	}
	if r.Body == nil || r.Body == http.NoBody {
		if optional {
			return nil
		}
		return badRequest("a JSON request body is required")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			return problemFor(err)
		case errors.Is(err, io.EOF) && optional:
			return nil
		case errors.Is(err, io.EOF):
			return badRequest("a JSON request body is required")
		}
		return badRequest("malformed JSON body: " + err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return problemFor(err)
		}
		return badRequest("the body must contain exactly one JSON value")
	}
	return nil
}

// queryInt reads an integer query parameter. A missing required parameter or a
// value that is not an integer is a 400.
func queryInt(r *http.Request, name string, required bool) (v int64, present bool, err error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		if required {
			return 0, false, badRequest("missing query parameter", issue(name, "is required"))
		}
		return 0, false, nil
	}
	v, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil {
		return 0, false, badRequest("malformed query parameter", issue(name, "must be an integer"))
	}
	return v, true, nil
}

func requireField[T any](name string, p *T) error {
	if p == nil {
		return badRequest("missing request field", issue(name, "is required"))
	}
	return nil
}
