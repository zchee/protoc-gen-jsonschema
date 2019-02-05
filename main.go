// Copyright 2018 The protoc-gen-jsonschema Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command protoc-gen-jsonschema protoc plugin which converts .proto to JSON Schema.
// The feature supports to generates the enum values for the redhat-developer/yaml-language-server spec JSON Schema.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/alecthomas/jsonschema"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/golang/protobuf/protoc-gen-go/generator"
	pluginpb "github.com/golang/protobuf/protoc-gen-go/plugin"
	"github.com/xeipuuv/gojsonschema"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	flagAllowNullValues              bool
	flagDisallowAdditionalProperties bool
	flagDisallowBigIntsAsStrings     bool
	flagDebug                        bool
	flagVersion                      bool
)

func init() {
	flag.BoolVar(&flagVersion, "version", false, "Print version")
	flag.Parse()

	cfg := zap.NewDevelopmentConfig()
	cfg.Level.SetLevel(zap.InfoLevel)
	cfg.EncoderConfig.EncodeTime = nil
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	cfg.EncoderConfig.LineEnding = zapcore.DefaultLineEnding
	if flagDebug {
		cfg.Level.SetLevel(zap.DebugLevel)
	}
	l, err := cfg.Build(zap.AddCaller())
	if err != nil {
		panic(fmt.Errorf("zap.cfg.Build: %+v", err))
	}
	zap.ReplaceGlobals(l)
}

func main() {
	defer zap.L().Sync()

	if flagVersion {
		fmt.Printf("%s:\n\tversion: %s\n", os.Args[0], Version)
		return
	}

	zap.S().Debug("Processing code generator request")
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		zap.S().Fatalf("failed to reading input from stdin: %v", err)
	}

	g := generator.New()
	if err := proto.Unmarshal(data, g.Request); err != nil {
		zap.S().Fatalf("failed to parsing input proto file: %v", err)
	}

	if len(g.Request.FileToGenerate) == 0 {
		zap.S().Fatal("no files to generate")
	}

	g.CommandLineParameters(g.Request.GetParameter())
	if parameter := g.Request.GetParameter(); parameter != "" {
		for _, param := range strings.Split(parameter, ",") {
			parts := strings.Split(param, "=")
			if len(parts) > 2 {
				zap.S().Infof("invalid parameter: %q", param)
				continue
			}

			switch parts[0] {
			case "allow_null_values":
				flagAllowNullValues = true
			case "debug":
				flagDebug = true
			case "disallow_additional_properties":
				flagDisallowAdditionalProperties = true
			case "disallow_bigints_as_strings":
				flagDisallowBigIntsAsStrings = true
			default:
				zap.S().Warnf("unknown parameter: %q", param)
			}
		}
	}

	g.Response, err = convert(g.Request)
	if err != nil {
		zap.S().Fatalf("failed to convert proto to jsonschema: %v", err)
	}

	// Generate the protobufs
	g.GenerateAllFiles()

	zap.S().Debug("Serializing code generator response")
	data, err = proto.Marshal(g.Response)
	if err != nil {
		zap.S().Fatalf("failed to marshal output proto: %v", err)
	}

	_, err = os.Stdout.Write(data)
	if err != nil {
		zap.S().Fatalf("failed to write response: %v", err)
	}

	zap.S().Info("succeeded to process code generator request")
}

var (
	globalPkg = &ProtoPackage{
		name:     "",
		parent:   nil,
		children: make(map[string]*ProtoPackage),
		types:    make(map[string]*descriptor.DescriptorProto),
	}
	globalPkgMu sync.RWMutex
)

// ProtoPackage describes a package of Protobuf, which is an container of message types.
type ProtoPackage struct {
	name     string
	parent   *ProtoPackage
	children map[string]*ProtoPackage
	types    map[string]*descriptor.DescriptorProto
}

func registerType(pkgName *string, msg *descriptor.DescriptorProto) {
	globalPkgMu.RLock()
	defer globalPkgMu.RUnlock()

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
					types:    make(map[string]*descriptor.DescriptorProto),
				}
				pkg.children[node] = child
			}
			pkg = child
		}
	}
	pkg.types[msg.GetName()] = msg
}

func relativelyLookupNestedType(desc *descriptor.DescriptorProto, name string) (*descriptor.DescriptorProto, bool) {
	components := strings.Split(name, ".")
componentLoop:
	for _, component := range components {
		for _, nested := range desc.GetNestedType() {
			if nested.GetName() == component {
				desc = nested
				continue componentLoop
			}
		}
		zap.S().Infof("no such nested message %s in %s", component, desc.GetName())
		return nil, false
	}

	return desc, true
}

func (pkg *ProtoPackage) relativelyLookupType(name string) (*descriptor.DescriptorProto, bool) {
	components := strings.SplitN(name, ".", 2)
	switch len(components) {
	case 0:
		zap.S().Debug("empty message name")
		return nil, false
	case 1:
		found, ok := pkg.types[components[0]]
		return found, ok
	case 2:
		zap.S().Debugf("looking for %s in %s at %s (%v)", components[1], components[0], pkg.name, pkg)
		if child, ok := pkg.children[components[0]]; ok {
			found, ok := child.relativelyLookupType(components[1])
			return found, ok
		}
		if msg, ok := pkg.types[components[0]]; ok {
			found, ok := relativelyLookupNestedType(msg, components[1])
			return found, ok
		}
		zap.S().Infof("no such package nor message %s in %s", components[0], pkg.name)
		return nil, false
	default:
		zap.S().Fatal("not reached")
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

func (pkg *ProtoPackage) lookupType(name string) (*descriptor.DescriptorProto, bool) {
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
func convertEnumType(enum *descriptor.EnumDescriptorProto) (jsonschema.Type, error) {
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
	ProtoTypeBool     = descriptor.FieldDescriptorProto_TYPE_BOOL
	ProtoTypeBytes    = descriptor.FieldDescriptorProto_TYPE_BYTES
	ProtoTypeDouble   = descriptor.FieldDescriptorProto_TYPE_DOUBLE
	ProtoTypeEnum     = descriptor.FieldDescriptorProto_TYPE_ENUM
	ProtoTypeFixed32  = descriptor.FieldDescriptorProto_TYPE_FIXED32
	ProtoTypeFixed64  = descriptor.FieldDescriptorProto_TYPE_FIXED64
	ProtoTypeFloat    = descriptor.FieldDescriptorProto_TYPE_FLOAT
	ProtoTypeGroup    = descriptor.FieldDescriptorProto_TYPE_GROUP
	ProtoTypeInt32    = descriptor.FieldDescriptorProto_TYPE_INT32
	ProtoTypeInt64    = descriptor.FieldDescriptorProto_TYPE_INT64
	ProtoTypeMessage  = descriptor.FieldDescriptorProto_TYPE_MESSAGE
	ProtoTypeSfixed32 = descriptor.FieldDescriptorProto_TYPE_SFIXED32
	ProtoTypeSfixed64 = descriptor.FieldDescriptorProto_TYPE_SFIXED64
	ProtoTypeSint32   = descriptor.FieldDescriptorProto_TYPE_SINT32
	ProtoTypeSint64   = descriptor.FieldDescriptorProto_TYPE_SINT64
	ProtoTypeString   = descriptor.FieldDescriptorProto_TYPE_STRING
	ProtoTypeUint32   = descriptor.FieldDescriptorProto_TYPE_UINT32
	ProtoTypeUint64   = descriptor.FieldDescriptorProto_TYPE_UINT64
)

var (
	keyTrue  = []byte("true")
	keyFalse = []byte("false")
)

// convertField convert a proto "field".
func convertField(pkg *ProtoPackage, desc *descriptor.FieldDescriptorProto, dp *descriptor.DescriptorProto) (*jsonschema.Type, error) {
	jsonSchemaType := &jsonschema.Type{
		Properties: make(map[string]*jsonschema.Type),
	}

	switch desc.GetType() {
	case ProtoTypeDouble, ProtoTypeFloat:
		if flagAllowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_NUMBER},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_NUMBER
		}

	case ProtoTypeInt32, ProtoTypeUint32, ProtoTypeFixed32, ProtoTypeSfixed32, ProtoTypeSint32:
		if flagAllowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_INTEGER},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_INTEGER
		}

	case ProtoTypeInt64, ProtoTypeUint64, ProtoTypeFixed64, ProtoTypeSfixed64, ProtoTypeSint64:
		jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_INTEGER})
		if !flagDisallowBigIntsAsStrings {
			jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_STRING})
		}
		if flagAllowNullValues {
			jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_NULL})
		}

	case ProtoTypeString,
		descriptor.FieldDescriptorProto_TYPE_BYTES:
		if flagAllowNullValues {
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
		if flagAllowNullValues {
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
		if flagAllowNullValues {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_BOOLEAN},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_BOOLEAN
		}

	case ProtoTypeGroup, ProtoTypeMessage:
		jsonSchemaType.Type = gojsonschema.TYPE_OBJECT
		if desc.GetLabel() == descriptor.FieldDescriptorProto_LABEL_OPTIONAL {
			jsonSchemaType.AdditionalProperties = keyTrue
		}
		if desc.GetLabel() == descriptor.FieldDescriptorProto_LABEL_REQUIRED {
			jsonSchemaType.AdditionalProperties = keyFalse
		}

	default:
		return nil, fmt.Errorf("unrecognized field type: %s", desc.GetType().String())
	}

	if desc.GetLabel() == descriptor.FieldDescriptorProto_LABEL_REPEATED && jsonSchemaType.Type != gojsonschema.TYPE_OBJECT {
		jsonSchemaType.Items = &jsonschema.Type{
			Type:  jsonSchemaType.Type,
			OneOf: jsonSchemaType.OneOf,
		}
		if flagAllowNullValues {
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

		recursedJSONSchemaType, err := convertMessageType(pkg, recordType)
		if err != nil {
			return nil, err
		}

		if desc.GetLabel() == descriptor.FieldDescriptorProto_LABEL_REPEATED {
			jsonSchemaType.Items = &recursedJSONSchemaType
			jsonSchemaType.Type = gojsonschema.TYPE_ARRAY
		} else {
			jsonSchemaType.Properties = recursedJSONSchemaType.Properties
		}

		if flagAllowNullValues {
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
func convertMessageType(pkg *ProtoPackage, msg *descriptor.DescriptorProto) (jsonschema.Type, error) {
	jsonSchemaType := jsonschema.Type{
		Properties: make(map[string]*jsonschema.Type),
		Version:    jsonschema.Version,
	}

	if flagAllowNullValues {
		jsonSchemaType.OneOf = []*jsonschema.Type{
			{Type: gojsonschema.TYPE_NULL},
			{Type: gojsonschema.TYPE_OBJECT},
		}
	} else {
		jsonSchemaType.Type = gojsonschema.TYPE_OBJECT
	}

	if flagDisallowAdditionalProperties {
		jsonSchemaType.AdditionalProperties = keyFalse
	} else {
		jsonSchemaType.AdditionalProperties = keyTrue
	}

	zap.S().Debugf("Converting message: %s", proto.MarshalTextString(msg))
	for _, fieldDesc := range msg.GetField() {
		recursedJSONSchemaType, err := convertField(pkg, fieldDesc, msg)
		if err != nil {
			zap.S().Errorf("Failed to convert field %s in %s: %v", fieldDesc.GetName(), msg.GetName(), err)
			return jsonSchemaType, err
		}
		jsonSchemaType.Properties[fieldDesc.GetName()] = recursedJSONSchemaType
	}

	return jsonSchemaType, nil
}

// convertFile converts a proto file into a JSON-Schema.
func convertFile(file *descriptor.FileDescriptorProto) (resp []*pluginpb.CodeGeneratorResponse_File, err error) {
	protoFileName := path.Base(file.GetName())

	switch len(file.GetMessageType()) {
	case 0:
		if len(file.GetEnumType()) > 1 {
			zap.S().Warnf("protoc-gen-jsonschema will create multiple ENUM schemas (%d) from one proto file (%s)", len(file.GetEnumType()), protoFileName)
		}

		for _, enum := range file.GetEnumType() {
			jsonSchemaFileName := fmt.Sprintf("%s.jsonschema", enum.GetName())
			zap.S().Infof("generating JSON-schema for stand-alone ENUM (%v) in file [%s] => %s", enum.GetName(), protoFileName, jsonSchemaFileName)

			enumJSONSchema, err := convertEnumType(enum)
			if err != nil {
				zap.S().Errorf("failed to convert %s: %v", protoFileName, err)
				return nil, err
			}

			jsonSchemaJSON, err := json.MarshalIndent(enumJSONSchema, "", "    ")
			if err != nil {
				zap.S().Errorf("failed to encode jsonSchema: %v", err)
				return nil, err
			}

			respFile := &pluginpb.CodeGeneratorResponse_File{
				Name:    proto.String(jsonSchemaFileName),
				Content: proto.String(string(jsonSchemaJSON)),
			}
			resp = append(resp, respFile)
		}
	default:
		zap.S().Warnf("protoc-gen-jsonschema will create multiple MESSAGE schemas (%d) from one proto file (%s)", len(file.GetMessageType()), protoFileName)

		globalPkgMu.RLock()
		pkg, ok := globalPkg.relativelyLookupPackage(file.GetPackage())
		globalPkgMu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("no such package found: %s", file.GetPackage())
		}

		for _, msg := range file.GetMessageType() {
			jsonSchemaFileName := fmt.Sprintf("%s.jsonschema", msg.GetName())
			zap.S().Infof("generating JSON-schema for MESSAGE (%s) in file [%s] => %s", msg.GetName(), protoFileName, jsonSchemaFileName)

			messageJSONSchema, err := convertMessageType(pkg, msg)
			if err != nil {
				zap.S().Errorf("failed to convert %s: %v", protoFileName, err)
				return nil, err
			}

			jsonSchemaJSON, err := json.MarshalIndent(messageJSONSchema, "", "    ")
			if err != nil {
				zap.S().Errorf("failed to encode jsonSchema: %v", err)
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

func convert(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	seenTargets := make(map[string]bool)
	for _, file := range req.GetFileToGenerate() {
		seenTargets[file] = true
	}

	resp := &pluginpb.CodeGeneratorResponse{}
	for _, file := range req.GetProtoFile() {
		for _, msg := range file.GetMessageType() {
			zap.S().Debugf("loading a message type %s from package %s", msg.GetName(), file.GetPackage())
			registerType(file.Package, msg)
		}
	}
	for _, file := range req.GetProtoFile() {
		if _, ok := seenTargets[file.GetName()]; ok {
			zap.S().Debugf("converting file (%v)", file.GetName())
			converted, err := convertFile(file)
			if err != nil {
				resp.Error = proto.String(fmt.Sprintf("Failed to convert %s: %v", file.GetName(), err))
				return resp, err
			}
			resp.File = append(resp.File, converted...)
		}
	}

	return resp, nil
}
