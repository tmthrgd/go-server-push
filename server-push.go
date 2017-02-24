// Copyright 2017 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package main

import (
	"log"
	"net/http"
	"strings"
	"unicode"
)

const pushSentinalHeader = "X-H2-Push"

type serverPusherResponseWriter struct {
	http.ResponseWriter
	http.Pusher

	opts *http.PushOptions

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

	return w.Push(path[1:len(path)-1], w.opts)
}

type serverPusher struct {
	http.Handler
}

func (s serverPusher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := w.(http.Pusher)
	if !ok {
		s.Handler.ServeHTTP(w, r)
		return
	}

	s.Handler.ServeHTTP(&serverPusherResponseWriter{
		ResponseWriter: w,
		Pusher:         p,

		opts: &http.PushOptions{
			Header: http.Header{
				pushSentinalHeader: []string{"1"},
			},
		},
	}, r)
}
