package app

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func decodeUpstreamResponse(resp *http.Response) (*http.Response, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = resp.Body.Close()

	decodedBody, decoded, err := decodeResponseBody(resp.Header.Get("Content-Encoding"), body)
	if err != nil {
		return nil, err
	}
	return decodedResponseClone(resp, decodedBody, decoded), nil
}

func decodeResponseBody(encoding string, body []byte) ([]byte, bool, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return body, false, nil
	case "gzip":
		return readDecodedBody(gzip.NewReader(bytes.NewReader(body)))
	case "deflate":
		return readDecodedBody(zlib.NewReader(bytes.NewReader(body)))
	case "br":
		decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
		if err != nil {
			return nil, false, err
		}
		return decoded, true, nil
	case "zstd":
		reader, err := zstd.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, false, fmt.Errorf("create zstd reader: %w", err)
		}
		defer reader.Close()
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return nil, false, fmt.Errorf("read zstd response: %w", err)
		}
		return decoded, true, nil
	default:
		return body, false, nil
	}
}

func readDecodedBody(reader io.ReadCloser, err error) ([]byte, bool, error) {
	if err != nil {
		return nil, false, err
	}
	defer reader.Close()

	decoded, err := io.ReadAll(reader)
	if err != nil {
		return nil, false, err
	}
	return decoded, true, nil
}

func decodedResponseClone(resp *http.Response, body []byte, decoded bool) *http.Response {
	cloned := *resp
	cloned.Header = resp.Header.Clone()
	if decoded {
		cloned.Header.Del("Content-Encoding")
	}
	cloned.Header.Set("Content-Length", strconv.Itoa(len(body)))
	cloned.ContentLength = int64(len(body))
	cloned.Body = io.NopCloser(bytes.NewReader(body))
	return &cloned
}
