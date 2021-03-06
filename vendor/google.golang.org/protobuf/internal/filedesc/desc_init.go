// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package filedesc

import (
	"google.golang.org/protobuf/internal/encoding/wire"
	"google.golang.org/protobuf/internal/fieldnum"
	pref "google.golang.org/protobuf/reflect/protoreflect"
)

// fileRaw is a data struct used when initializing a file descriptor from
// a raw FileDescriptorProto.
type fileRaw struct {
	builder       DescBuilder
	allEnums      []Enum
	allMessages   []Message
	allExtensions []Extension
	allServices   []Service
}

func newRawFile(db DescBuilder) *File {
	fd := &File{fileRaw: fileRaw{builder: db}}
	fd.initDecls(db.NumEnums, db.NumMessages, db.NumExtensions, db.NumServices)
	fd.unmarshalSeed(db.RawDescriptor)

	// Extended message targets are eagerly resolved since registration
	// needs this information at program init time.
	for i := range fd.allExtensions {
		xd := &fd.allExtensions[i]
		xd.L1.Extendee = fd.resolveMessageDependency(xd.L1.Extendee, listExtTargets, int32(i))
	}

	fd.checkDecls()
	return fd
}

// initDecls pre-allocates slices for the exact number of enums, messages
// (including map entries), extensions, and services declared in the proto file.
// This is done to avoid regrowing the slice, which would change the address
// for any previously seen declaration.
//
// The alloc methods "allocates" slices by pulling from the capacity.
func (fd *File) initDecls(numEnums, numMessages, numExtensions, numServices int32) {
	fd.allEnums = make([]Enum, 0, numEnums)
	fd.allMessages = make([]Message, 0, numMessages)
	fd.allExtensions = make([]Extension, 0, numExtensions)
	fd.allServices = make([]Service, 0, numServices)
}

func (fd *File) allocEnums(n int) []Enum {
	total := len(fd.allEnums)
	es := fd.allEnums[total : total+n]
	fd.allEnums = fd.allEnums[:total+n]
	return es
}
func (fd *File) allocMessages(n int) []Message {
	total := len(fd.allMessages)
	ms := fd.allMessages[total : total+n]
	fd.allMessages = fd.allMessages[:total+n]
	return ms
}
func (fd *File) allocExtensions(n int) []Extension {
	total := len(fd.allExtensions)
	xs := fd.allExtensions[total : total+n]
	fd.allExtensions = fd.allExtensions[:total+n]
	return xs
}
func (fd *File) allocServices(n int) []Service {
	total := len(fd.allServices)
	xs := fd.allServices[total : total+n]
	fd.allServices = fd.allServices[:total+n]
	return xs
}

// checkDecls performs a sanity check that the expected number of expected
// declarations matches the number that were found in the descriptor proto.
func (fd *File) checkDecls() {
	switch {
	case len(fd.allEnums) != cap(fd.allEnums):
	case len(fd.allMessages) != cap(fd.allMessages):
	case len(fd.allExtensions) != cap(fd.allExtensions):
	case len(fd.allServices) != cap(fd.allServices):
	default:
		return
	}
	panic("mismatching cardinality")
}

func (fd *File) unmarshalSeed(b []byte) {
	nb := getNameBuilder()
	defer putNameBuilder(nb)

	var prevField pref.FieldNumber
	var numEnums, numMessages, numExtensions, numServices int
	var posEnums, posMessages, posExtensions, posServices int
	b0 := b
	for len(b) > 0 {
		num, typ, n := wire.ConsumeTag(b)
		b = b[n:]
		switch typ {
		case wire.BytesType:
			v, m := wire.ConsumeBytes(b)
			b = b[m:]
			switch num {
			case fieldnum.FileDescriptorProto_Syntax:
				switch string(v) {
				case "proto2":
					fd.L1.Syntax = pref.Proto2
				case "proto3":
					fd.L1.Syntax = pref.Proto3
				default:
					panic("invalid syntax")
				}
			case fieldnum.FileDescriptorProto_Name:
				fd.L1.Path = nb.MakeString(v)
			case fieldnum.FileDescriptorProto_Package:
				fd.L1.Package = pref.FullName(nb.MakeString(v))
			case fieldnum.FileDescriptorProto_EnumType:
				if prevField != fieldnum.FileDescriptorProto_EnumType {
					if numEnums > 0 {
						panic("non-contiguous repeated field")
					}
					posEnums = len(b0) - len(b) - n - m
				}
				numEnums++
			case fieldnum.FileDescriptorProto_MessageType:
				if prevField != fieldnum.FileDescriptorProto_MessageType {
					if numMessages > 0 {
						panic("non-contiguous repeated field")
					}
					posMessages = len(b0) - len(b) - n - m
				}
				numMessages++
			case fieldnum.FileDescriptorProto_Extension:
				if prevField != fieldnum.FileDescriptorProto_Extension {
					if numExtensions > 0 {
						panic("non-contiguous repeated field")
					}
					posExtensions = len(b0) - len(b) - n - m
				}
				numExtensions++
			case fieldnum.FileDescriptorProto_Service:
				if prevField != fieldnum.FileDescriptorProto_Service {
					if numServices > 0 {
						panic("non-contiguous repeated field")
					}
					posServices = len(b0) - len(b) - n - m
				}
				numServices++
			}
			prevField = num
		default:
			m := wire.ConsumeFieldValue(num, typ, b)
			b = b[m:]
			prevField = -1 // ignore known field numbers of unknown wire type
		}
	}

	// If syntax is missing, it is assumed to be proto2.
	if fd.L1.Syntax == 0 {
		fd.L1.Syntax = pref.Proto2
	}

	// Must allocate all declarations before parsing each descriptor type
	// to ensure we handled all descriptors in "flattened ordering".
	if numEnums > 0 {
		fd.L1.Enums.List = fd.allocEnums(numEnums)
	}
	if numMessages > 0 {
		fd.L1.Messages.List = fd.allocMessages(numMessages)
	}
	if numExtensions > 0 {
		fd.L1.Extensions.List = fd.allocExtensions(numExtensions)
	}
	if numServices > 0 {
		fd.L1.Services.List = fd.allocServices(numServices)
	}

	if numEnums > 0 {
		b := b0[posEnums:]
		for i := range fd.L1.Enums.List {
			_, n := wire.ConsumeVarint(b)
			v, m := wire.ConsumeBytes(b[n:])
			fd.L1.Enums.List[i].unmarshalSeed(v, nb, fd, fd, i)
			b = b[n+m:]
		}
	}
	if numMessages > 0 {
		b := b0[posMessages:]
		for i := range fd.L1.Messages.List {
			_, n := wire.ConsumeVarint(b)
			v, m := wire.ConsumeBytes(b[n:])
			fd.L1.Messages.List[i].unmarshalSeed(v, nb, fd, fd, i)
			b = b[n+m:]
		}
	}
	if numExtensions > 0 {
		b := b0[posExtensions:]
		for i := range fd.L1.Extensions.List {
			_, n := wire.ConsumeVarint(b)
			v, m := wire.ConsumeBytes(b[n:])
			fd.L1.Extensions.List[i].unmarshalSeed(v, nb, fd, fd, i)
			b = b[n+m:]
		}
	}
	if numServices > 0 {
		b := b0[posServices:]
		for i := range fd.L1.Services.List {
			_, n := wire.ConsumeVarint(b)
			v, m := wire.ConsumeBytes(b[n:])
			fd.L1.Services.List[i].unmarshalSeed(v, nb, fd, fd, i)
			b = b[n+m:]
		}
	}
}

func (ed *Enum) unmarshalSeed(b []byte, nb *nameBuilder, pf *File, pd pref.Descriptor, i int) {
	ed.L0.ParentFile = pf
	ed.L0.Parent = pd
	ed.L0.Index = i

	var numValues int
	for b := b; len(b) > 0; {
		num, typ, n := wire.ConsumeTag(b)
		b = b[n:]
		switch typ {
		case wire.BytesType:
			v, m := wire.ConsumeBytes(b)
			b = b[m:]
			switch num {
			case fieldnum.EnumDescriptorProto_Name:
				ed.L0.FullName = nb.AppendFullName(pd.FullName(), v)
			case fieldnum.EnumDescriptorProto_Value:
				numValues++
			}
		default:
			m := wire.ConsumeFieldValue(num, typ, b)
			b = b[m:]
		}
	}

	// Only construct enum value descriptors for top-level enums since
	// they are needed for registration.
	if pd != pf {
		return
	}
	ed.L1.eagerValues = true
	ed.L2 = new(EnumL2)
	ed.L2.Values.List = make([]EnumValue, numValues)
	for i := 0; len(b) > 0; {
		num, typ, n := wire.ConsumeTag(b)
		b = b[n:]
		switch typ {
		case wire.BytesType:
			v, m := wire.ConsumeBytes(b)
			b = b[m:]
			switch num {
			case fieldnum.EnumDescriptorProto_Value:
				ed.L2.Values.List[i].unmarshalFull(v, nb, pf, ed, i)
				i++
			}
		default:
			m := wire.ConsumeFieldValue(num, typ, b)
			b = b[m:]
		}
	}
}

func (md *Message) unmarshalSeed(b []byte, nb *nameBuilder, pf *File, pd pref.Descriptor, i int) {
	md.L0.ParentFile = pf
	md.L0.Parent = pd
	md.L0.Index = i

	var prevField pref.FieldNumber
	var numEnums, numMessages, numExtensions int
	var posEnums, posMessages, posExtensions int
	b0 := b
	for len(b) > 0 {
		num, typ, n := wire.ConsumeTag(b)
		b = b[n:]
		switch typ {
		case wire.BytesType:
			v, m := wire.ConsumeBytes(b)
			b = b[m:]
			switch num {
			case fieldnum.DescriptorProto_Name:
				md.L0.FullName = nb.AppendFullName(pd.FullName(), v)
			case fieldnum.DescriptorProto_EnumType:
				if prevField != fieldnum.DescriptorProto_EnumType {
					if numEnums > 0 {
						panic("non-contiguous repeated field")
					}
					posEnums = len(b0) - len(b) - n - m
				}
				numEnums++
			case fieldnum.DescriptorProto_NestedType:
				if prevField != fieldnum.DescriptorProto_NestedType {
					if numMessages > 0 {
						panic("non-contiguous repeated field")
					}
					posMessages = len(b0) - len(b) - n - m
				}
				numMessages++
			case fieldnum.DescriptorProto_Extension:
				if prevField != fieldnum.DescriptorProto_Extension {
					if numExtensions > 0 {
						panic("non-contiguous repeated field")
					}
					posExtensions = len(b0) - len(b) - n - m
				}
				numExtensions++
			}
			prevField = num
		default:
			m := wire.ConsumeFieldValue(num, typ, b)
			b = b[m:]
			prevField = -1 // ignore known field numbers of unknown wire type
		}
	}

	// Must allocate all declarations before parsing each descriptor type
	// to ensure we handled all descriptors in "flattened ordering".
	if numEnums > 0 {
		md.L1.Enums.List = pf.allocEnums(numEnums)
	}
	if numMessages > 0 {
		md.L1.Messages.List = pf.allocMessages(numMessages)
	}
	if numExtensions > 0 {
		md.L1.Extensions.List = pf.allocExtensions(numExtensions)
	}

	if numEnums > 0 {
		b := b0[posEnums:]
		for i := range md.L1.Enums.List {
			_, n := wire.ConsumeVarint(b)
			v, m := wire.ConsumeBytes(b[n:])
			md.L1.Enums.List[i].unmarshalSeed(v, nb, pf, md, i)
			b = b[n+m:]
		}
	}
	if numMessages > 0 {
		b := b0[posMessages:]
		for i := range md.L1.Messages.List {
			_, n := wire.ConsumeVarint(b)
			v, m := wire.ConsumeBytes(b[n:])
			md.L1.Messages.List[i].unmarshalSeed(v, nb, pf, md, i)
			b = b[n+m:]
		}
	}
	if numExtensions > 0 {
		b := b0[posExtensions:]
		for i := range md.L1.Extensions.List {
			_, n := wire.ConsumeVarint(b)
			v, m := wire.ConsumeBytes(b[n:])
			md.L1.Extensions.List[i].unmarshalSeed(v, nb, pf, md, i)
			b = b[n+m:]
		}
	}
}

func (xd *Extension) unmarshalSeed(b []byte, nb *nameBuilder, pf *File, pd pref.Descriptor, i int) {
	xd.L0.ParentFile = pf
	xd.L0.Parent = pd
	xd.L0.Index = i

	for len(b) > 0 {
		num, typ, n := wire.ConsumeTag(b)
		b = b[n:]
		switch typ {
		case wire.VarintType:
			v, m := wire.ConsumeVarint(b)
			b = b[m:]
			switch num {
			case fieldnum.FieldDescriptorProto_Number:
				xd.L1.Number = pref.FieldNumber(v)
			case fieldnum.FieldDescriptorProto_Type:
				xd.L1.Kind = pref.Kind(v)
			}
		case wire.BytesType:
			v, m := wire.ConsumeBytes(b)
			b = b[m:]
			switch num {
			case fieldnum.FieldDescriptorProto_Name:
				xd.L0.FullName = nb.AppendFullName(pd.FullName(), v)
			case fieldnum.FieldDescriptorProto_Extendee:
				xd.L1.Extendee = PlaceholderMessage(nb.MakeFullName(v))
			}
		default:
			m := wire.ConsumeFieldValue(num, typ, b)
			b = b[m:]
		}
	}
}

func (sd *Service) unmarshalSeed(b []byte, nb *nameBuilder, pf *File, pd pref.Descriptor, i int) {
	sd.L0.ParentFile = pf
	sd.L0.Parent = pd
	sd.L0.Index = i

	for len(b) > 0 {
		num, typ, n := wire.ConsumeTag(b)
		b = b[n:]
		switch typ {
		case wire.BytesType:
			v, m := wire.ConsumeBytes(b)
			b = b[m:]
			switch num {
			case fieldnum.ServiceDescriptorProto_Name:
				sd.L0.FullName = nb.AppendFullName(pd.FullName(), v)
			}
		default:
			m := wire.ConsumeFieldValue(num, typ, b)
			b = b[m:]
		}
	}
}
