// Copyright 2017 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License that can be found in
// the LICENSE file.

package serverpush

import "net/http"

type redirectResponseWriter struct {
	http.ResponseWriter
	http.Pusher
	req *http.Request

	opts http.PushOptions

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

	w.opts.Header = headers(&w.opts, w.req)

	if err := w.Push(location[0], &w.opts); err != nil && err != http.ErrNotSupported {
		server := w.req.Context().Value(http.ServerContextKey).(*http.Server)
		if server.ErrorLog != nil {
			server.ErrorLog.Println(err)
		}
	}

	w.ResponseWriter.WriteHeader(code)
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
	pusher, ok := w.(http.Pusher)
	if !ok {
		pr.Handler.ServeHTTP(w, r)
		return
	}

	pr.Handler.ServeHTTP(&redirectResponseWriter{
		ResponseWriter: w,
		Pusher:         pusher,
		req:            r,

		opts: pr.opts,
	}, r)
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
