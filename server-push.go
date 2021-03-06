// Copyright 2017 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package serverpush

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync"
	"unicode"

	"github.com/golang/gddo/httputil/header"
	"github.com/tmthrgd/httputils"
	"github.com/willf/bloom"
)

var (
	flateReaderPool sync.Pool
	flateWriterPool sync.Pool

	bufferPool = &sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
)

type options struct {
	m, k        uint
	cookie      *http.Cookie
	pushOptions http.PushOptions
}

type pushResponseWriter struct {
	http.ResponseWriter
	req *http.Request

	opts *options

	bloom *bloom.BloomFilter

	wroteHeader bool
}

func (w *pushResponseWriter) WriteHeader(code int) {
	wroteHeader := w.wroteHeader
	w.wroteHeader = true

	if wroteHeader || code == http.StatusNotModified {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	h := w.Header()
	links := header.ParseList(h, "Link")

	if len(links) == 0 {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	opts := w.opts.pushOptions
	opts.Header = headers(&opts, w.req)

	rest := links[:0]
	var pushed []string

	for _, link := range links {
		didPush, err := w.pushLink(&opts, link)
		if err == http.ErrNotSupported {
			rest = links
			break
		} else if err != nil {
			httputils.RequestLogf(w.req, "go-server-push: error pushing link %q: %#v", link, err)
		}

		if didPush {
			pushed = append(pushed, link)
		} else {
			rest = append(rest, link)
		}
	}

	h["Link"] = rest
	h[pushedHeader] = pushed

	if len(pushed) != 0 {
		if err := w.saveBloomFilter(); err != nil {
			httputils.RequestLogf(w.req, "go-server-push: error saving bloom filter: %#v", err)
		}
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *pushResponseWriter) WriteString(s string) (n int, err error) {
	return io.WriteString(w.ResponseWriter, s)
}

func isFieldSeparator(r rune) bool {
	return r == ';' || unicode.IsSpace(r)
}

func (w *pushResponseWriter) pushLink(opts *http.PushOptions, link string) (pushed bool, err error) {
	fields := strings.FieldsFunc(link, isFieldSeparator)
	if len(fields) < 2 {
		return false, nil
	}

	path, fields := fields[0], fields[1:]
	if len(path) < 4 || path[0] != '<' ||
		path[1] != '/' || path[2] == '/' ||
		path[len(path)-1] != '>' {
		return false, nil
	}

	var isPreload bool
	for _, field := range fields {
		switch field {
		case "rel=preload", `rel="preload"`:
			isPreload = true
		case "nopush":
			return false, nil
		}
	}

	if !isPreload {
		return false, nil
	}

	path = path[1 : len(path)-1]

	if w.bloom == nil {
		w.loadBloomFilter()
	}

	if w.bloom.TestString(path) {
		return false, nil
	}

	if err := w.Push(path, opts); err != nil {
		return false, err
	}

	w.bloom.AddString(path)
	return true, nil
}

func (w *pushResponseWriter) loadBloomFilter() {
	c, err := w.req.Cookie(w.opts.cookie.Name)
	if err != nil || c.Value == "" {
		w.bloom = bloom.New(w.opts.m, w.opts.k)
		return
	}

	sr := strings.NewReader(c.Value)
	b64r := base64.NewDecoder(base64.RawStdEncoding, sr)

	fr, _ := flateReaderPool.Get().(io.ReadCloser)
	if fr == nil {
		fr = flate.NewReader(b64r)
	} else if err := fr.(flate.Resetter).Reset(b64r, nil); err != nil {
		panic(err)
	}

	w.bloom = new(bloom.BloomFilter)
	if _, err := w.bloom.ReadFrom(fr); err != nil {
		httputils.RequestLogf(w.req, "go-server-push: error loading bloom filter: %#v", err)

		w.bloom = bloom.New(w.opts.m, w.opts.k)
	}

	if err := fr.Close(); err != nil {
		httputils.RequestLogf(w.req, "go-server-push: error closing flate writer: %#v", err)
	}

	flateReaderPool.Put(fr)
}

func (w *pushResponseWriter) saveBloomFilter() (err error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	b64w := base64.NewEncoder(base64.RawStdEncoding, buf)

	fw, _ := flateWriterPool.Get().(*flate.Writer)
	if fw != nil {
		fw.Reset(b64w)
	} else if fw, err = flate.NewWriter(b64w, flate.BestSpeed); err != nil {
		return
	}

	if _, err = w.bloom.WriteTo(fw); err != nil {
		return
	}

	if err = fw.Close(); err != nil {
		return
	}

	flateWriterPool.Put(fw)

	if err = b64w.Close(); err != nil {
		return
	}

	c := *w.opts.cookie
	c.Value = buf.String()
	http.SetCookie(w, &c)

	buf.Reset()
	bufferPool.Put(buf)
	return
}

func (w *pushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.ResponseWriter.(http.Pusher).Push(target, opts)
}

func (w *pushResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type pushHandler struct {
	http.Handler
	options
}

func (s *pushHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Pusher); !ok {
		s.Handler.ServeHTTP(w, r)
		return
	}

	prw := &pushResponseWriter{
		ResponseWriter: w,
		req:            r,

		opts: &s.options,
	}

	var rw http.ResponseWriter = prw

	if _, ok := w.(http.CloseNotifier); ok {
		rw = closeNotifyPushResponseWriter{prw}
	}

	s.Handler.ServeHTTP(rw, r)
}

// Options specifies additional options to change the
// behaviour of the handler.
type Options struct {
	Cookie      *http.Cookie
	PushOptions *http.PushOptions
}

// New wraps the given http.Handler in a push aware handler.
func New(m, k uint, handler http.Handler, opts *Options) Handler {
	s := &pushHandler{
		Handler: handler,
		options: options{
			m: m,
			k: k,
		},
	}

	if opts != nil && opts.Cookie != nil {
		s.cookie = opts.Cookie
	} else {
		s.cookie = &http.Cookie{
			Name: defaultCookieName,

			MaxAge:   7776000,
			Secure:   true,
			HttpOnly: true,
		}
	}

	if opts != nil && opts.PushOptions != nil {
		s.pushOptions = *opts.PushOptions
	}

	return s
}

// Wrapper returns a Middleware that calls New.
func Wrapper(m, k uint, opts *Options) Middleware {
	return func(h http.Handler) http.Handler {
		return New(m, k, h, opts)
	}
}

// EstimateParameters estimates requirements for m and k.
func EstimateParameters(n uint, p float64) (m, k uint) {
	return bloom.EstimateParameters(n, p)
}

// This struct is intentionally small (1 pointer wide) so as to
// fit inside an interface{} without causing an allocaction.
type closeNotifyPushResponseWriter struct{ *pushResponseWriter }

var _ http.CloseNotifier = closeNotifyPushResponseWriter{}

func (w closeNotifyPushResponseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.(http.CloseNotifier).CloseNotify()
}
