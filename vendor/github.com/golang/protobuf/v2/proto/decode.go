// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style.
// license that can be found in the LICENSE file.

package proto

import (
	"errors"

	"github.com/golang/protobuf/v2/internal/encoding/wire"
	"github.com/golang/protobuf/v2/internal/pragma"
	"github.com/golang/protobuf/v2/reflect/protoreflect"
)

// UnmarshalOptions configures the unmarshaler.
//
// Example usage:
//   err := UnmarshalOptions{DiscardUnknown: true}.Unmarshal(b, m)
type UnmarshalOptions struct {
	// If DiscardUnknown is set, unknown fields are ignored.
	DiscardUnknown bool

	pragma.NoUnkeyedLiterals
}

// Unmarshal parses the wire-format message in b and places the result in m.
func Unmarshal(b []byte, m Message) error {
	return UnmarshalOptions{}.Unmarshal(b, m)
}

// Unmarshal parses the wire-format message in b and places the result in m.
func (o UnmarshalOptions) Unmarshal(b []byte, m Message) error {
	// TODO: Reset m?
	return o.unmarshalMessage(b, m.ProtoReflect())
}

func (o UnmarshalOptions) unmarshalMessage(b []byte, m protoreflect.Message) error {
	messageType := m.Type()
	fieldTypes := messageType.Fields()
	knownFields := m.KnownFields()
	unknownFields := m.UnknownFields()
	for len(b) > 0 {
		// Parse the tag (field number and wire type).
		num, wtyp, tagLen := wire.ConsumeTag(b)
		if tagLen < 0 {
			return wire.ParseError(tagLen)
		}

		// Parse the field value.
		fieldType := fieldTypes.ByNumber(num)
		if fieldType == nil {
			fieldType = knownFields.ExtensionTypes().ByNumber(num)
		}
		var err error
		var valLen int
		switch {
		case fieldType == nil:
			err = errUnknown
		case fieldType.Cardinality() != protoreflect.Repeated:
			valLen, err = o.unmarshalScalarField(b[tagLen:], wtyp, num, knownFields, fieldType)
		case !fieldType.IsMap():
			valLen, err = o.unmarshalList(b[tagLen:], wtyp, num, knownFields.Get(num).List(), fieldType.Kind())
		default:
			valLen, err = o.unmarshalMap(b[tagLen:], wtyp, num, knownFields.Get(num).Map(), fieldType)
		}
		if err == errUnknown {
			valLen = wire.ConsumeFieldValue(num, wtyp, b[tagLen:])
			if valLen < 0 {
				return wire.ParseError(valLen)
			}
			unknownFields.Set(num, append(unknownFields.Get(num), b[:tagLen+valLen]...))
		} else if err != nil {
			return err
		}
		b = b[tagLen+valLen:]
	}
	// TODO: required field checks
	return nil
}

func (o UnmarshalOptions) unmarshalScalarField(b []byte, wtyp wire.Type, num wire.Number, knownFields protoreflect.KnownFields, field protoreflect.FieldDescriptor) (n int, err error) {
	v, n, err := o.unmarshalScalar(b, wtyp, num, field.Kind())
	if err != nil {
		return 0, err
	}
	switch field.Kind() {
	case protoreflect.GroupKind, protoreflect.MessageKind:
		// Messages are merged with any existing message value,
		// unless the message is part of a oneof.
		//
		// TODO: C++ merges into oneofs, while v1 does not.
		// Evaluate which behavior to pick.
		var m protoreflect.Message
		if knownFields.Has(num) && field.OneofType() == nil {
			m = knownFields.Get(num).Message()
		} else {
			m = knownFields.NewMessage(num)
			knownFields.Set(num, protoreflect.ValueOf(m))
		}
		if err := o.unmarshalMessage(v.Bytes(), m); err != nil {
			return 0, err
		}
	default:
		// Non-message scalars replace the previous value.
		knownFields.Set(num, v)
	}
	return n, nil
}

func (o UnmarshalOptions) unmarshalMap(b []byte, wtyp wire.Type, num wire.Number, mapv protoreflect.Map, field protoreflect.FieldDescriptor) (n int, err error) {
	if wtyp != wire.BytesType {
		return 0, errUnknown
	}
	b, n = wire.ConsumeBytes(b)
	if n < 0 {
		return 0, wire.ParseError(n)
	}
	var (
		keyField = field.MessageType().Fields().ByNumber(1)
		valField = field.MessageType().Fields().ByNumber(2)
		key      protoreflect.Value
		val      protoreflect.Value
		haveKey  bool
		haveVal  bool
	)
	switch valField.Kind() {
	case protoreflect.GroupKind, protoreflect.MessageKind:
		val = protoreflect.ValueOf(mapv.NewMessage())
	}
	// Map entries are represented as a two-element message with fields
	// containing the key and value.
	for len(b) > 0 {
		num, wtyp, n := wire.ConsumeTag(b)
		if n < 0 {
			return 0, wire.ParseError(n)
		}
		b = b[n:]
		err = errUnknown
		switch num {
		case 1:
			key, n, err = o.unmarshalScalar(b, wtyp, num, keyField.Kind())
			if err != nil {
				break
			}
			haveKey = true
		case 2:
			var v protoreflect.Value
			v, n, err = o.unmarshalScalar(b, wtyp, num, valField.Kind())
			if err != nil {
				break
			}
			switch valField.Kind() {
			case protoreflect.GroupKind, protoreflect.MessageKind:
				if err := o.unmarshalMessage(v.Bytes(), val.Message()); err != nil {
					return 0, err
				}
			default:
				val = v
			}
			haveVal = true
		}
		if err == errUnknown {
			n = wire.ConsumeFieldValue(num, wtyp, b)
			if n < 0 {
				return 0, wire.ParseError(n)
			}
		} else if err != nil {
			return 0, err
		}
		b = b[n:]
	}
	// Every map entry should have entries for key and value, but this is not strictly required.
	if !haveKey {
		key = keyField.Default()
	}
	if !haveVal {
		switch valField.Kind() {
		case protoreflect.GroupKind, protoreflect.MessageKind:
			// Trigger required field checks by unmarshaling an empty message.
			if err := o.unmarshalMessage(nil, val.Message()); err != nil {
				return 0, err
			}
		default:
			val = valField.Default()
		}
	}
	mapv.Set(key.MapKey(), val)
	return n, nil
}

// errUnknown is used internally to indicate fields which should be added
// to the unknown field set of a message. It is never returned from an exported
// function.
var errUnknown = errors.New("unknown")
