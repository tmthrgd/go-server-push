// Copyright 2017 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package serverpush

import "net/http"

const (
	sentinelHeader    = "X-H2-Push"
	defaultCookieName = "X-H2-Push"
)

var proxyHeaders = []string{
	"Accept-Encoding",
	"Accept-Language",
	"Cache-Control",
	"User-Agent",
}

func headers(opts *http.PushOptions, r *http.Request) http.Header {
	h := make(http.Header, 1+len(opts.Header)+len(proxyHeaders))
	for k, v := range opts.Header {
		h[k] = v
	}

	for _, k := range proxyHeaders {
		h[k] = r.Header[k]
	}

	h[sentinelHeader] = []string{"1"}
	return h
}

// IsPush returns true iff the request was pushed by this
// package.
func IsPush(r *http.Request) bool {
	_, isPush := r.Header[sentinelHeader]
	return isPush
}

type responseWriterFlusherPusher interface {
	http.ResponseWriter
	http.Flusher
	http.Pusher
}

type closeNotifierResponseWriter struct {
	responseWriterFlusherPusher
	http.CloseNotifier
}
