// Copyright 2018 The protoc-gen-jsonschema Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command protoc-gen-jsonschema protoc plugin which converts .proto to JSON Schema.
// The feature supports to generates the enum values for the redhat-developer/yaml-language-server spec JSON Schema.
package main

import (
	"flag"
	"log"

	"google.golang.org/protobuf/protogen"

	"github.com/zchee/protoc-gen-jsonschema/pkg/genjsonschema"
)

var (
	flagVersion bool
)

func init() {
	log.SetFlags(log.Lshortfile)

	flag.BoolVar(&flagVersion, "version", false, "Print version")
}

func main() {
	var (
		flags flag.FlagSet
		opts  = &protogen.Options{
			ParamFunc: flags.Set,
		}
	)
	flags.Bool("allow_null_values", false, "allow null values")
	flags.Bool("disallow_additional_properties", false, "disallow additional_properties")
	flags.Bool("disallow_bigints_as_strings", false, "disallow bigints as strings")
	flags.Bool("debug", false, "debug mode")

	// flag.Parse()
	// if flagVersion {
	// 	fmt.Printf("%s:\n\tversion: %s\n", os.Args[0], genjsonschema.Version)
	// 	return
	// }

	protogen.Run(opts, func(gen *protogen.Plugin) error {
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}

			filename := f.GeneratedFilenamePrefix + ".jsonschema"
			f.GoImportPath = protogen.GoImportPath(f.Proto.GetName())
			g := gen.NewGeneratedFile(filename, f.GoImportPath)
			genjsonschema.Gen(gen, f, g)
		}
		return nil
	})
}
