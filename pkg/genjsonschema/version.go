// Copyright 2019 The protoc-gen-jsonschema Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package genjsonschema

// tag is version of protoc-gen-jsonschema.
//
// This variables can overridden using `-X main.tag` during release builds.
var tag string

// gitCommit is commit hash of protoc-gen-jsonschema.
//
// This variables can overridden using `-X main.gitCommit` during release builds.
var gitCommit string

// Version is the protoc-gen-jsonschema version.
var Version = tag + "@" + gitCommit
