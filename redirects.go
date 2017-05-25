// Copyright 2017 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License that can be found in
// the LICENSE file.

package serverpush

import (
	"io"
	"net/http"
)

type redirectResponseWriter struct {
	http.ResponseWriter
	req *http.Request

	opts *http.PushOptions

	wroteHeader bool
}

func (w *redirectResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	w.wroteHeader = true

	location := w.Header()["Location"]
	if code < 300 || code >= 400 || len(location) != 1 ||
		location[0] == "" || location[0][0] != '/' {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	opts := *w.opts
	opts.Header = headers(w.opts, w.req)

	if err := w.Push(location[0], &opts); err != nil && err != http.ErrNotSupported {
		server := w.req.Context().Value(http.ServerContextKey).(*http.Server)
		if server.ErrorLog != nil {
			server.ErrorLog.Println(err)
		}
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *redirectResponseWriter) WriteString(s string) (n int, err error) {
	return io.WriteString(w.ResponseWriter, s)
}

func (w *redirectResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.ResponseWriter.(http.Pusher).Push(target, opts)
}

func (w *redirectResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type redirects struct {
	http.Handler
	opts http.PushOptions
}

func (pr *redirects) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Pusher); !ok {
		pr.Handler.ServeHTTP(w, r)
		return
	}

	rrw := &redirectResponseWriter{
		ResponseWriter: w,
		req:            r,

		opts: &pr.opts,
	}

	var rw http.ResponseWriter = rrw

	if cn, ok := w.(http.CloseNotifier); ok {
		rw = &closeNotifierResponseWriter{rrw, cn}
	}

	pr.Handler.ServeHTTP(rw, r)
}

// Redirects wraps the given http.Handler and pushes the Location
// of redirects to clients.
func Redirects(h http.Handler, opts *http.PushOptions) http.Handler {
	r := &redirects{
		Handler: h,
	}

	if opts != nil {
		r.opts = *opts
	}

	return r
}
