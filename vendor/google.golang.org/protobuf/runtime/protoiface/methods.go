// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package protoiface contains types referenced or implemented by messages.
//
// WARNING: This package should only be imported by message implementations.
// The functionality found in this package should be accessed through
// higher-level abstractions provided by the proto package.
package protoiface

import (
	"google.golang.org/protobuf/internal/pragma"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Methoder is an optional interface implemented by generated messages to
// provide fast-path implementations of various operations.
type Methoder interface {
	XXX_Methods() *Methods // may return nil
}

// Methods is a set of optional fast-path implementations of various operations.
type Methods struct {
	// Flags indicate support for optional features.
	Flags MethodFlag

	// MarshalAppend appends the wire-format encoding of m to b, returning the result.
	// It does not perform required field checks.
	MarshalAppend func(b []byte, m protoreflect.ProtoMessage, opts MarshalOptions) ([]byte, error)

	// Size returns the size in bytes of the wire-format encoding of m.
	Size func(m protoreflect.ProtoMessage) int

	// Unmarshal parses the wire-format message in b and places the result in m.
	// It does not reset m or perform required field checks.
	Unmarshal func(b []byte, m protoreflect.ProtoMessage, opts UnmarshalOptions) error

	// IsInitialized returns an error if any required fields in m are not set.
	IsInitialized func(m protoreflect.ProtoMessage) error

	pragma.NoUnkeyedLiterals
}

// MethodFlag indicates support for optional fast-path features.
type MethodFlag int64

const (
	// MethodFlagDeterministicMarshal indicates support for deterministic marshaling.
	MethodFlagDeterministicMarshal MethodFlag = 1 << iota
)

// MarshalOptions configure the marshaler.
//
// This type is identical to the one in package proto.
type MarshalOptions struct {
	AllowPartial  bool
	Deterministic bool
	UseCachedSize bool

	pragma.NoUnkeyedLiterals
}

// UnmarshalOptions configures the unmarshaler.
//
// This type is identical to the one in package proto.
type UnmarshalOptions struct {
	AllowPartial   bool
	DiscardUnknown bool
	Resolver       interface {
		protoregistry.ExtensionTypeResolver
	}

	pragma.NoUnkeyedLiterals
}
