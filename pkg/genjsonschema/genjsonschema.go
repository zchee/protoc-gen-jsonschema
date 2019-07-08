// Copyright 2019 The protoc-gen-jsonschema Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package genjsonschema

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/alecthomas/jsonschema"
	descriptorpb "github.com/golang/protobuf/v2/types/descriptor"
	pluginpb "github.com/golang/protobuf/v2/types/plugin"
	"github.com/xeipuuv/gojsonschema"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var (
	atom = zap.NewAtomicLevelAt(zap.InfoLevel) // INFO level by default
	log  *zap.SugaredLogger
)

func init() {
	cfg := zap.NewDevelopmentConfig()

	cfg.Level = atom
	cfg.DisableStacktrace = true
	cfg.EncoderConfig.EncodeTime = nil
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	cfg.EncoderConfig.LineEnding = zapcore.DefaultLineEnding

	l, err := cfg.Build(zap.AddCaller())
	if err != nil {
		panic(fmt.Errorf("zap.cfg.Build: %+v", err))
	}
	log = l.Named("genjsonschema").Sugar()
}

type fileinfo struct {
	*protogen.File

	allEnums         []*protogen.Enum
	allEnumsByPtr    map[*protogen.Enum]int // value is index into allEnums
	allMessages      []*protogen.Message
	allMessagesByPtr map[*protogen.Message]int // value is index into allMessages

	opts *options
}

type options struct {
	allowNullValues              bool
	disallowAdditionalProperties bool
	disallowBigIntsAsStrings     bool
	debug                        bool
}

func Gen(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile) {
	defer log.Sync()

	f := &fileinfo{
		File: file,
		opts: new(options),
	}

	if parameter := gen.Request.GetParameter(); parameter != "" {
		for _, param := range strings.Split(parameter, ",") {
			parts := strings.Split(param, "=")
			if len(parts) > 2 {
				log.Warnf("invalid parameter: %q", param)
				continue
			}

			switch parts[0] {
			case "allow_null_values":
				f.opts.allowNullValues = true
			case "debug":
				f.opts.debug = true
				atom.SetLevel(zap.DebugLevel)
			case "disallow_additional_properties":
				f.opts.disallowAdditionalProperties = true
			case "disallow_bigints_as_strings":
				f.opts.disallowBigIntsAsStrings = true
			default:
				log.Warnf("unknown parameter: %q", param)
			}
		}
	}

	f.allEnums = append(f.allEnums, f.Enums...)
	f.allMessages = append(f.allMessages, f.Messages...)
	walkMessages(f.Messages, func(m *protogen.Message) {
		f.allEnums = append(f.allEnums, m.Enums...)
		f.allMessages = append(f.allMessages, m.Messages...)
	})

	req := gen.Request
	desc := file.Desc

	resp, err := f.convertfn(req, desc)
	if err != nil {
		log.Fatalf("failed to convert proto to jsonschema: %v", err)
	}

	log.Debugf("resp: %v\n", resp)

	seen := make(map[string]bool)
	rf := protoregistry.NewFiles(file.Desc)
	rf.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for _, file := range req.GetFileToGenerate() {
			seen[file] = true
		}
		return true
	})

	// if _, err := f.convert(gen.Request); err != nil {
	// 	log.Fatalf("failed to convert proto to jsonschema: %+v", err)
	// }

	// log.Debugf("Gen", zap.Any("resp", resp))

	// g := generator.New()
	// g.Request = req
	//
	// if len(g.Request.FileToGenerate) == 0 {
	// 	log.Fatal("no files to generate")
	// }
	//
	// g.CommandLineParameters(g.Request.GetParameter())
	// if parameter := g.Request.GetParameter(); parameter != "" {
	// 	for _, param := range strings.Split(parameter, ",") {
	// 		parts := strings.Split(param, "=")
	// 		if len(parts) > 2 {
	// 			log.Warnf("invalid parameter: %q", param)
	// 			continue
	// 		}
	//
	// 		switch parts[0] {
	// 		case "allow_null_values":
	// 			flagAllowNullValues = true
	// 		case "debug":
	// 			atom.SetLevel(zap.DebugLevel)
	// 		case "disallow_additional_properties":
	// 			flagDisallowAdditionalProperties = true
	// 		case "disallow_bigints_as_strings":
	// 			flagDisallowBigIntsAsStrings = true
	// 		default:
	// 			log.Warnf("unknown parameter: %q", param)
	// 		}
	// 	}
	// }
	//
	// g.Response, err = convert(g.Request)
	// if err != nil {
	// 	log.Fatalf("failed to convert proto to jsonschema: %+v", err)
	// }
	//
	// g.GenerateAllFiles()
	//
	// return resp, nil
	log.Info("succeeded to process code generator request")
}

func (f *fileinfo) convertfn(req *pluginpb.CodeGeneratorRequest, desc protoreflect.FileDescriptor) (*pluginpb.CodeGeneratorResponse, error) {
	seen := make(map[string]bool)
	for _, gf := range req.GetFileToGenerate() {
		seen[gf] = true
	}

	return nil, nil
}

func (f *fileinfo) convert(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	seenTargets := make(map[string]bool)
	for _, file := range req.GetFileToGenerate() {
		seenTargets[file] = true
	}

	resp := &pluginpb.CodeGeneratorResponse{}
	for _, file := range req.GetProtoFile() {
		for _, msg := range file.GetMessageType() {
			log.Debugf("loading a message type %s from package %s", msg.GetName(), file.GetPackage())
			registerType(file.Package, msg)
		}
	}
	for _, file := range req.GetProtoFile() {
		if _, ok := seenTargets[file.GetName()]; ok {
			log.Debugf("converting file (%v)", file.GetName())
			converted, err := f.convertFile(file)
			if err != nil {
				resp.Error = proto.String(fmt.Sprintf("Failed to convert %s: %v", file.GetName(), err))
				return resp, err
			}
			resp.File = append(resp.File, converted...)
		}
	}

	return resp, nil
}

// walkMessages calls f on each message and all of its descendants.
func walkMessages(messages []*protogen.Message, f func(*protogen.Message)) {
	for _, m := range messages {
		f(m)
		walkMessages(m.Messages, f)
	}
}

// ProtoPackage describes a package of Protobuf, which is an container of message types.
type ProtoPackage struct {
	name     string
	parent   *ProtoPackage
	children map[string]*ProtoPackage
	types    map[string]*descriptorpb.DescriptorProto
}

var (
	globalPkg = &ProtoPackage{
		name:     "",
		parent:   nil,
		children: make(map[string]*ProtoPackage),
		types:    make(map[string]*descriptorpb.DescriptorProto),
	}

	globalPkgMu sync.RWMutex
)

func registerType(pkgName *string, msg *descriptorpb.DescriptorProto) {
	globalPkgMu.RLock()
	defer globalPkgMu.RUnlock()

	log.Debugf("pkgName: %s\n", *pkgName)
	pkg := globalPkg
	if pkgName != nil {
		for _, node := range strings.Split(*pkgName, ".") {
			if pkg == globalPkg && node == "" {
				// skips leading "."
				continue
			}
			child, ok := pkg.children[node]
			if !ok {
				child = &ProtoPackage{
					name:     pkg.name + "." + node,
					parent:   pkg,
					children: make(map[string]*ProtoPackage),
					types:    make(map[string]*descriptorpb.DescriptorProto),
				}
				pkg.children[node] = child
			}
			pkg = child
		}
	}
	pkg.types[msg.GetName()] = msg
}

func relativelyLookupNestedType(desc *descriptorpb.DescriptorProto, name string) (*descriptorpb.DescriptorProto, bool) {
	components := strings.Split(name, ".")
componentLoop:
	for _, component := range components {
		for _, nested := range desc.GetNestedType() {
			if nested.GetName() == component {
				desc = nested
				continue componentLoop
			}
		}
		log.Warnf("no such nested message %s in %s", component, desc.GetName())
		return nil, false
	}

	return desc, true
}

func (pkg *ProtoPackage) relativelyLookupType(name string) (*descriptorpb.DescriptorProto, bool) {
	components := strings.SplitN(name, ".", 2)
	switch len(components) {
	case 0:
		log.Debug("empty message name")
		return nil, false
	case 1:
		found, ok := pkg.types[components[0]]
		return found, ok
	case 2:
		log.Debugf("looking for %s in %s at %s (%v)", components[1], components[0], pkg.name, pkg)
		if child, ok := pkg.children[components[0]]; ok {
			found, ok := child.relativelyLookupType(components[1])
			return found, ok
		}
		if msg, ok := pkg.types[components[0]]; ok {
			found, ok := relativelyLookupNestedType(msg, components[1])
			return found, ok
		}
		log.Infof("no such package nor message %s in %s", components[0], pkg.name)
		return nil, false
	default:
		log.Fatal("not reached")
		return nil, false
	}
}

func (pkg *ProtoPackage) relativelyLookupPackage(name string) (*ProtoPackage, bool) {
	components := strings.Split(name, ".")
	for _, c := range components {
		var ok bool
		pkg, ok = pkg.children[c]
		if !ok {
			return nil, false
		}
	}

	return pkg, true
}

func (pkg *ProtoPackage) lookupType(name string) (*descriptorpb.DescriptorProto, bool) {
	globalPkgMu.RLock()
	defer globalPkgMu.RUnlock()

	if strings.HasPrefix(name, ".") {
		return globalPkg.relativelyLookupType(name[1:])
	}

	for ; pkg != nil; pkg = pkg.parent {
		if desc, ok := pkg.relativelyLookupType(name); ok {
			return desc, ok
		}
	}

	return nil, false
}

// convertEnumType converts a proto "ENUM" into a JSON-Schema.
func convertEnumType(enum *descriptorpb.EnumDescriptorProto) (jsonschema.Type, error) {
	jsonSchemaType := jsonschema.Type{
		Version: jsonschema.Version,
	}

	jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: "string"})
	jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: "integer"})

	for _, enumValue := range enum.Value {
		jsonSchemaType.Enum = append(jsonSchemaType.Enum, enumValue.Name)
		jsonSchemaType.Enum = append(jsonSchemaType.Enum, enumValue.Number)
	}

	return jsonSchemaType, nil
}

// alias of descriptor.FieldDescriptorProto_TYPE.
const (
	ProtoTypeBool     = descriptorpb.FieldDescriptorProto_TYPE_BOOL
	ProtoTypeBytes    = descriptorpb.FieldDescriptorProto_TYPE_BYTES
	ProtoTypeDouble   = descriptorpb.FieldDescriptorProto_TYPE_DOUBLE
	ProtoTypeEnum     = descriptorpb.FieldDescriptorProto_TYPE_ENUM
	ProtoTypeFixed32  = descriptorpb.FieldDescriptorProto_TYPE_FIXED32
	ProtoTypeFixed64  = descriptorpb.FieldDescriptorProto_TYPE_FIXED64
	ProtoTypeFloat    = descriptorpb.FieldDescriptorProto_TYPE_FLOAT
	ProtoTypeGroup    = descriptorpb.FieldDescriptorProto_TYPE_GROUP
	ProtoTypeInt32    = descriptorpb.FieldDescriptorProto_TYPE_INT32
	ProtoTypeInt64    = descriptorpb.FieldDescriptorProto_TYPE_INT64
	ProtoTypeMessage  = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	ProtoTypeSfixed32 = descriptorpb.FieldDescriptorProto_TYPE_SFIXED32
	ProtoTypeSfixed64 = descriptorpb.FieldDescriptorProto_TYPE_SFIXED64
	ProtoTypeSint32   = descriptorpb.FieldDescriptorProto_TYPE_SINT32
	ProtoTypeSint64   = descriptorpb.FieldDescriptorProto_TYPE_SINT64
	ProtoTypeString   = descriptorpb.FieldDescriptorProto_TYPE_STRING
	ProtoTypeUint32   = descriptorpb.FieldDescriptorProto_TYPE_UINT32
	ProtoTypeUint64   = descriptorpb.FieldDescriptorProto_TYPE_UINT64
)

var (
	keyTrue  = []byte("true")
	keyFalse = []byte("false")
)

// convertField convert a proto "field".
func (f *fileinfo) convertField(pkg *ProtoPackage, desc *descriptorpb.FieldDescriptorProto, dp *descriptorpb.DescriptorProto) (*jsonschema.Type, error) {
	jsonSchemaType := &jsonschema.Type{
		Properties: make(map[string]*jsonschema.Type),
	}

	switch desc.GetType() {
	case ProtoTypeDouble, ProtoTypeFloat:
		if f.opts.allowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_NUMBER},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_NUMBER
		}

	case ProtoTypeInt32, ProtoTypeUint32, ProtoTypeFixed32, ProtoTypeSfixed32, ProtoTypeSint32:
		if f.opts.allowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_INTEGER},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_INTEGER
		}

	case ProtoTypeInt64, ProtoTypeUint64, ProtoTypeFixed64, ProtoTypeSfixed64, ProtoTypeSint64:
		jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_INTEGER})
		if !f.opts.disallowBigIntsAsStrings {
			jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_STRING})
		}
		if f.opts.allowNullValues {
			jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_NULL})
		}

	case ProtoTypeString,
		descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		if f.opts.allowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_STRING},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_STRING
		}

	case ProtoTypeEnum:
		jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_STRING})
		jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_INTEGER})
		if f.opts.allowNullValues {
			jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_NULL})
		}

		for _, enumDescriptor := range dp.GetEnumType() {
			for _, enumValue := range enumDescriptor.Value {
				fullFieldName := fmt.Sprintf(".%s.%s", *dp.Name, *enumDescriptor.Name)

				if strings.HasSuffix(desc.GetTypeName(), fullFieldName) {
					jsonSchemaType.Enum = append(jsonSchemaType.Enum, enumValue.Name)
					jsonSchemaType.Enum = append(jsonSchemaType.Enum, enumValue.Number)
				}
			}
		}

	case ProtoTypeBool:
		if f.opts.allowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_BOOLEAN},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_BOOLEAN
		}

	case ProtoTypeGroup, ProtoTypeMessage:
		jsonSchemaType.Type = gojsonschema.TYPE_OBJECT
		if desc.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL {
			jsonSchemaType.AdditionalProperties = keyTrue
		}
		if desc.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REQUIRED {
			jsonSchemaType.AdditionalProperties = keyFalse
		}

	default:
		return nil, fmt.Errorf("unrecognized field type: %s", desc.GetType().String())
	}

	if desc.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED && jsonSchemaType.Type != gojsonschema.TYPE_OBJECT {
		jsonSchemaType.Items = &jsonschema.Type{
			Type:  jsonSchemaType.Type,
			OneOf: jsonSchemaType.OneOf,
		}
		if f.opts.allowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_ARRAY},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_ARRAY
			jsonSchemaType.OneOf = []*jsonschema.Type{}
		}

		return jsonSchemaType, nil
	}

	if jsonSchemaType.Type == gojsonschema.TYPE_OBJECT {
		recordType, ok := pkg.lookupType(desc.GetTypeName())
		if !ok {
			return nil, fmt.Errorf("no such message type named %s", desc.GetTypeName())
		}

		recursedJSONSchemaType, err := f.convertMessageType(pkg, recordType)
		if err != nil {
			return nil, err
		}

		if desc.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
			jsonSchemaType.Items = &recursedJSONSchemaType
			jsonSchemaType.Type = gojsonschema.TYPE_ARRAY
		} else {
			jsonSchemaType.Properties = recursedJSONSchemaType.Properties
		}

		if f.opts.allowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: jsonSchemaType.Type},
			}
			jsonSchemaType.Type = ""
		}
	}

	return jsonSchemaType, nil
}

// convertMessageType converts a proto "MESSAGE" into a JSON-Schema.
func (f *fileinfo) convertMessageType(pkg *ProtoPackage, msg *descriptorpb.DescriptorProto) (jsonschema.Type, error) {
	jsonSchemaType := jsonschema.Type{
		Properties: make(map[string]*jsonschema.Type),
		Version:    jsonschema.Version,
	}

	if f.opts.allowNullValues {
		jsonSchemaType.OneOf = []*jsonschema.Type{
			{Type: gojsonschema.TYPE_NULL},
			{Type: gojsonschema.TYPE_OBJECT},
		}
	} else {
		jsonSchemaType.Type = gojsonschema.TYPE_OBJECT
	}

	if f.opts.disallowAdditionalProperties {
		jsonSchemaType.AdditionalProperties = keyFalse
	} else {
		jsonSchemaType.AdditionalProperties = keyTrue
	}

	// log.Debugf("Converting message: %s", proto.MarshalTextString(msg))
	for _, fieldDesc := range msg.GetField() {
		recursedJSONSchemaType, err := f.convertField(pkg, fieldDesc, msg)
		if err != nil {
			log.Errorf("Failed to convert field %s in %s: %v", fieldDesc.GetName(), msg.GetName(), err)
			return jsonSchemaType, err
		}
		jsonSchemaType.Properties[fieldDesc.GetName()] = recursedJSONSchemaType
	}

	return jsonSchemaType, nil
}

// convertFile converts a proto file into a JSON-Schema.
func (f *fileinfo) convertFile(file *descriptorpb.FileDescriptorProto) (resp []*pluginpb.CodeGeneratorResponse_File, err error) {
	protoFileName := path.Base(file.GetName())

	switch len(file.GetMessageType()) {
	case 0:
		if len(file.GetEnumType()) > 1 {
			log.Warnf("protoc-gen-jsonschema will create multiple ENUM schemas (%d) from one proto file (%s)", len(file.GetEnumType()), protoFileName)
		}

		for _, enum := range file.GetEnumType() {
			jsonSchemaFileName := fmt.Sprintf("%s.jsonschema", enum.GetName())
			log.Infof("generating JSON-schema for stand-alone ENUM (%v) in file [%s] => %s", enum.GetName(), protoFileName, jsonSchemaFileName)

			enumJSONSchema, err := convertEnumType(enum)
			if err != nil {
				log.Errorf("failed to convert %s: %v", protoFileName, err)
				return nil, err
			}

			jsonSchemaJSON, err := json.MarshalIndent(enumJSONSchema, "", "    ")
			if err != nil {
				log.Errorf("failed to encode jsonSchema: %v", err)
				return nil, err
			}

			respFile := &pluginpb.CodeGeneratorResponse_File{
				Name:    proto.String(jsonSchemaFileName),
				Content: proto.String(string(jsonSchemaJSON)),
			}
			resp = append(resp, respFile)
		}
	default:
		log.Warnf("protoc-gen-jsonschema will create multiple MESSAGE schemas (%d) from one proto file (%s)", len(file.GetMessageType()), protoFileName)

		globalPkgMu.RLock()
		pkg, ok := globalPkg.relativelyLookupPackage(file.GetPackage())
		globalPkgMu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("no such package found: %s", file.GetPackage())
		}

		for _, msg := range file.GetMessageType() {
			jsonSchemaFileName := fmt.Sprintf("%s.jsonschema", msg.GetName())
			log.Debugf("generating JSON-schema for MESSAGE (%s) in file [%s] => %s", msg.GetName(), protoFileName, jsonSchemaFileName)

			messageJSONSchema, err := f.convertMessageType(pkg, msg)
			if err != nil {
				log.Errorf("failed to convert %s: %v", protoFileName, err)
				return nil, err
			}

			jsonSchemaJSON, err := json.MarshalIndent(messageJSONSchema, "", "    ")
			if err != nil {
				log.Errorf("failed to encode jsonSchema: %v", err)
				return nil, err
			}

			respFile := &pluginpb.CodeGeneratorResponse_File{
				Name:    proto.String(jsonSchemaFileName),
				Content: proto.String(string(jsonSchemaJSON)),
			}
			resp = append(resp, respFile)
		}
	}

	return resp, nil
}
