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

import "net/http"

// Handler is an alias to http.Handler for godoc.
type Handler = http.Handler

// Middleware represents a function that wraps an
// http.Handler.
type Middleware = func(http.Handler) http.Handler
