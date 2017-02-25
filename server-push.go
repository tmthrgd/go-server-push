// Copyright 2017 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

// Package serverpush implements a HTTP/2 Server Push
// aware http.Handler.
//
// It looks for Link headers in the response with
// rel=preload and will automatically push each
// linked resource. If the nopush attribute is
// included the resource will not be pushed.
//
// It uses a DEFLATE compressed bloom filter to store
// a probabilistic view of resources that have already
// been pushed to the client.
package serverpush

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/willf/bloom"
)

const (
	pushSentinalHeader = "X-H2-Push"
	defaultCookieName  = "X-H2-Push"
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

type serverPusherResponseWriter struct {
	http.ResponseWriter
	http.Pusher
	req *http.Request

	opts *http.PushOptions

	loadOnce sync.Once
	bloom    *bloom.BloomFilter
	didPush  bool
	m, k     uint
	cookie   string

	wroteHeader bool
}

func (w *serverPusherResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		w.ResponseWriter.WriteHeader(code)
		return
	}

	w.wroteHeader = true

outer:
	for _, link := range w.Header()["Link"] {
		for _, value := range strings.Split(link, ",") {
			if err := w.pushLink(value); err != nil {
				log.Println(err)
				break outer
			}
		}
	}

	if err := w.saveBloomFilter(); err != nil {
		log.Println(err)
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *serverPusherResponseWriter) pushLink(link string) error {
	fields := strings.FieldsFunc(link, func(r rune) bool {
		return r == ';' || unicode.IsSpace(r)
	})
	if len(fields) < 2 {
		return nil
	}

	path, fields := fields[0], fields[1:]
	if len(path) < 4 || path[0] != '<' ||
		path[1] != '/' || path[2] == '/' ||
		path[len(path)-1] != '>' {
		return nil
	}

	var isPreload bool
	for _, field := range fields {
		switch field {
		case "rel=preload", `rel="preload"`:
			isPreload = true
		case "nopush":
			return nil
		}
	}

	if !isPreload {
		return nil
	}

	path = path[1 : len(path)-1]

	w.loadOnce.Do(w.loadBloomFilter)
	if w.bloom.TestString(path) {
		return nil
	}

	if err := w.Push(path, w.opts); err != nil {
		return err
	}

	w.didPush = true
	w.bloom.AddString(path)
	return nil
}

func (w *serverPusherResponseWriter) loadBloomFilter() {
	c, err := w.req.Cookie(w.cookie)
	if err != nil || c.Value == "" {
		w.bloom = bloom.New(w.m, w.k)
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

	f := new(bloom.BloomFilter)
	if _, err := f.ReadFrom(fr); err != nil {
		log.Println(err)

		f = bloom.New(w.m, w.k)
	}

	if err := fr.Close(); err != nil {
		log.Println(err)
	} else {
		flateReaderPool.Put(fr)
	}

	w.bloom = f
}

func (w *serverPusherResponseWriter) saveBloomFilter() (err error) {
	if !w.didPush {
		return
	}

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

	http.SetCookie(w, &http.Cookie{
		Name:  w.cookie,
		Value: buf.String(),

		MaxAge:   int(90 * 24 * time.Hour / time.Second),
		Secure:   true,
		HttpOnly: true,
	})

	buf.Reset()
	bufferPool.Put(buf)
	return
}

type serverPusher struct {
	http.Handler

	m, k   uint
	cookie string
}

func (s *serverPusher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := w.(http.Pusher)
	if !ok {
		s.Handler.ServeHTTP(w, r)
		return
	}

	s.Handler.ServeHTTP(&serverPusherResponseWriter{
		ResponseWriter: w,
		Pusher:         p,
		req:            r,

		opts: &http.PushOptions{
			Header: http.Header{
				pushSentinalHeader: []string{"1"},
			},
		},

		m:      s.m,
		k:      s.k,
		cookie: s.cookie,
	}, r)
}

// NewWithCookie wraps the given http.Handler in a push aware
// handler, using a given cookie name to store the bloom filter
// in.
func NewWithCookie(m, k uint, cookie string, handler http.Handler) http.Handler {
	return &serverPusher{handler, m, k, cookie}
}

// New wraps the given http.Handler in a push aware handler.
func New(m, k uint, handler http.Handler) http.Handler {
	return NewWithCookie(m, k, defaultCookieName, handler)
}

// EstimateParameters estimates requirements for m and k.
func EstimateParameters(n uint, p float64) (m, k uint) {
	return bloom.EstimateParameters(n, p)
}
