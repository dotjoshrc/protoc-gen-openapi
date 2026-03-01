package main

import (
	"flag"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/dotjoshrc/protoc-gen-openapi/internal/generator"
)

func main() {
	var flags flag.FlagSet

	conf := generator.Config{
		ContentType:     flags.String("content_type", "application/json", "Default content-type for all paths."),
		DefaultResponse: flags.String("default_response", "", "Default response message to use for API responses not defined."),
		Description:     flags.String("description", "", "Description of the API."),
		Filename:        flags.String("filename", "openapi", "Name of the file generated without the extension."),
		Host:            flags.String("host", "", "Host to be used for all routes."),
		Ignore:          flags.String("ignore", "", "Packages to ignore."),
		Include:         flags.String("include", "", "Packages to include. Ignore overrides this."),
		JSONOutput:      flags.Bool("json_out", false, "Generate a JSON file instead of YAML."),
		Title:           flags.String("title", "", "Title of the API"),
		UseJSONNames:    flags.Bool("json_names", false, "Use JSON names instead of the proto names of fields."),
		Version:         flags.String("version", "0.0.1", "Version of the API."),
	}

	opts := protogen.Options{
		ParamFunc: flags.Set,
	}

	opts.Run(func(plugin *protogen.Plugin) error {
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL) | uint64(pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
		plugin.SupportedEditionsMinimum = descriptorpb.Edition_EDITION_PROTO2
		plugin.SupportedEditionsMaximum = descriptorpb.Edition_EDITION_2023

		return generator.New(plugin, conf).Run()
	})
}
