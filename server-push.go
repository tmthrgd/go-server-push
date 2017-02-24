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

const pushSentinalHeader = "X-Push"

type serverPusher struct {
	http.Handler
}

func (s serverPusher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Handler.ServeHTTP(w, r)

	p, ok0 := w.(http.Pusher)
	links, ok1 := w.Header()["Link"]
	if !ok0 || !ok1 {
		return
	}

	opts := &http.PushOptions{Header: make(http.Header)}
	opts.Header.Add(pushSentinalHeader, "")

	for _, link := range links {
		for _, value := range strings.Split(link, ",") {
			if err := s.pushLink(p, value, opts); err != nil {
				log.Println(err)
				return
			}
		}
	}
}

func (serverPusher) pushLink(p http.Pusher, link string, opts *http.PushOptions) error {
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

	return p.Push(path[1:len(path)-1], opts)
}
