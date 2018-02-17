// Copyright 2017 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License that can be found in
// the LICENSE file.

package serverpush

import (
	"io"
	"net/http"

	"github.com/tmthrgd/httputils"
)

type redirectResponseWriter struct {
	http.ResponseWriter
	req *http.Request

	opts *http.PushOptions
}

func (w *redirectResponseWriter) WriteHeader(code int) {
	req := w.req
	w.req = nil

	location := w.Header().Get("Location")
	if req == nil || code < 300 || code >= 400 ||
		location == "" || location[0] != '/' {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	opts := *w.opts
	opts.Header = headers(w.opts, req)

	if err := w.Push(location, &opts); err != nil && err != http.ErrNotSupported {
		httputils.RequestLogf(req, "go-server-push: error pushing resource %q: %#v", location, err)
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

	if _, ok := w.(http.CloseNotifier); ok {
		rw = closeNotifyRedirectsResponseWriter{rrw}
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

// RedirectsWrap returns a Middleware that calls Redirects.
func RedirectsWrap(opts *http.PushOptions) Middleware {
	return func(h http.Handler) http.Handler {
		return Redirects(h, opts)
	}
}

// This struct is intentionally small (1 pointer wide) so as to
// fit inside an interface{} without causing an allocaction.
type closeNotifyRedirectsResponseWriter struct{ *redirectResponseWriter }

var _ http.CloseNotifier = closeNotifyRedirectsResponseWriter{}

func (w closeNotifyRedirectsResponseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.(http.CloseNotifier).CloseNotify()
}
