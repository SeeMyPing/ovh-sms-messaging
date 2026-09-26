// Package httpapi holds what the HTTP SMS providers share.
package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// maxResponseSize bounds how much of a response is read.
const maxResponseSize = 1 << 20

// Do sends req and returns the response status and body. Transport errors
// do not include the request URL, which may carry credentials.
func Do(client *http.Client, req *http.Request) (int, []byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = fmt.Errorf("%s %s: %w", ue.Op, req.URL.Host, ue.Err)
		}
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, body, nil
}
