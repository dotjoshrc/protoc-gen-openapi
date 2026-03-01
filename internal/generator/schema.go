package generator

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	oapiv1 "github.com/dotjoshrc/protoc-gen-openapi/internal/gen/go/oapi/v1"
	"github.com/dotjoshrc/protoc-gen-openapi/internal/generator/util"
)

// addSchemasToDoc adds all non "request" and "response" messages as schemas to the OAPI doc.
func (g *Generator) addSchemasToDoc(doc *openapi3.T, messages []*protogen.Message) error {
	for _, message := range messages {
		messageName := util.FullName(message)

		if util.IsRequestMessage(message) || util.IsResponseMessage(message) {
			continue
		}

		messageSchemaRef := &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				Properties: make(openapi3.Schemas),
			},
		}

		err := g.buildSchema(doc, message, messageSchemaRef)
		if err != nil {
			return err
		}

		addSchema(doc, messageName, messageSchemaRef)
	}

	return nil
}

// getFieldName returns the raw field name or the JSON defined one if the config is set to use JSON
// names.
func (g *Generator) getFieldName(field *protogen.Field) string {
	if *g.config.UseJSONNames {
		return field.Desc.JSONName()
	}

	return string(field.Desc.Name())
}

// buildSchema takes a message and recursively builds out an OAPI schema.
func (g *Generator) buildSchema(doc *openapi3.T, message *protogen.Message, parent *openapi3.SchemaRef) error {
	if parent.Value.Required == nil {
		parent.Value.Required = make([]string, 0)
	}

	for _, field := range message.Fields {
		// Use the JSON name if defined.
		fieldName := g.getFieldName(field)

		fieldSchemaRef := &openapi3.SchemaRef{
			Value: newFieldSchema(field.Desc),
		}
		parsed := g.parseComments(field.Comments.Leading)
		fieldSchemaRef.Value.Description = parsed.Description

		// Apply example. This can be overridden below on the example option.
		if parsed.Example != "" {
			var example any
			exampleBytes := []byte(parsed.Example)

			if json.Valid(exampleBytes) {
				if err := json.Unmarshal(exampleBytes, &example); err != nil {
					return err
				}
			} else {
				example = parsed.Example
			}

			fieldSchemaRef.Value.Example = example
		}

		// Deprecated option.
		if standardOptions, ok := field.Desc.Options().(*descriptorpb.FieldOptions); ok {
			fieldSchemaRef.Value.Deprecated = standardOptions.GetDeprecated()
		}

		// Required option.
		extRequired := proto.GetExtension(field.Desc.Options(), oapiv1.E_Required)
		if extRequired != nil && extRequired != oapiv1.E_Required.InterfaceOf(oapiv1.E_Required.Zero()) {
			parent.Value.Required = append(parent.Value.Required, fieldName)
		}

		// Example option.
		extExample := proto.GetExtension(field.Desc.Options(), oapiv1.E_Example)
		if extExample != nil && extExample != oapiv1.E_Example.InterfaceOf(oapiv1.E_Example.Zero()) {
			parent.Value.Description = *extExample.(*string)
		}

		// Field options.
		err := g.setSchemaProperties(fieldSchemaRef.Value, parent.Value, field)
		if err != nil {
			return err
		}

		// TODO: Figure out how to merge the following 2 logical pieces together so most of it isn't
		// duplicated.
		if fieldSchemaRef.Value.Type == openapi3.TypeObject {
			fieldMessageName := util.FullName(field.Message)

			var isChild bool
			// Here we look for child messages first in case the name is the same as a top level
			// name.
			for _, childMessage := range message.Messages {
				if field.Message.Desc.FullName() == childMessage.Desc.FullName() {
					isChild = true
					err := g.buildSchema(doc, childMessage, fieldSchemaRef)
					if err != nil {
						return err
					}
				}
			}

			if !isChild {
				// If it's not a child message, it should exist in schemas, and we can then just do
				// a ref...
				if schemaExists(doc, fieldMessageName) {
					fieldSchemaRef.Ref = newSchemaRef(fieldMessageName)
				} else {
					// If it doesn't exist in schemas, then it's referenced elsewhere. We'll try to
					// snag it from our message map.
					msg := allMessages.Get(fieldMessageName)
					if msg != nil {
						// Use the message to build it out inline instead of using a ref.
						err := g.buildSchema(doc, msg, fieldSchemaRef)
						if err != nil {
							return err
						}
					}
				}
			}
		}

		if fieldSchemaRef.Value.Type == openapi3.TypeArray && fieldSchemaRef.Value.Items.Value.Type == "object" {
			// Array of objects to build out
			fieldMessageName := util.FullName(field.Message)

			var isChild bool
			for _, childMessage := range message.Messages {
				if field.Message.Desc.FullName() == childMessage.Desc.FullName() {
					isChild = true
					err := g.buildSchema(doc, childMessage, fieldSchemaRef.Value.Items)
					if err != nil {
						return err
					}
				}
			}

			if !isChild {
				// If it's not a child message, it should exist in schemas, and we can then just do
				// a ref...
				if schemaExists(doc, fieldMessageName) {
					fieldSchemaRef.Ref = newSchemaRef(fieldMessageName)
				} else {
					// If it doesn't exist in schemas, then it's referenced elsewhere. We'll try to
					// snag it from our message map.
					msg := allMessages.Get(fieldMessageName)
					if msg != nil {
						// Use the message to build it out inline instead of using a ref.
						err := g.buildSchema(doc, msg, fieldSchemaRef.Value.Items)
						if err != nil {
							return err
						}
					}
				}
			}
		}

		parent.Value.Properties[fieldName] = fieldSchemaRef
	}

	return nil
}

// addSchema adds the specified schema to the OAPI doc.
func addSchema(doc *openapi3.T, key string, value *openapi3.SchemaRef) {
	doc.Components.Schemas[key] = value
}

// schemaExists returns whether a schema exists or not on the doc.
func schemaExists(doc *openapi3.T, name string) bool {
	_, ok := doc.Components.Schemas[name]

	return ok
}

// newArraySchema returns a new schema for an array of the specified kind.
func newArraySchema(kind protoreflect.Kind) *openapi3.Schema {
	return &openapi3.Schema{
		Type:       openapi3.TypeArray,
		Properties: make(openapi3.Schemas),
		Items: &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				Type:       protoKindToAPIType(kind),
				Properties: make(openapi3.Schemas),
			},
		},
	}
}

// newSchemaRef returns a convenient schema reference.
func newSchemaRef(name string) string {
	return "#/components/schemas/" + name
}

// newFieldSchema returns a new OAPI represented schema for protobuf types on fields.
func newFieldSchema(field protoreflect.FieldDescriptor) *openapi3.Schema {
	kind := field.Kind()
	schema := &openapi3.Schema{
		Type:       protoKindToAPIType(kind),
		Properties: make(openapi3.Schemas),
	}

	if field.IsList() {
		schema.Type = openapi3.TypeArray

		return newArraySchema(kind)
	}

	return schema
}

// protoKindToAPIType returns an OAPI type based on the proto kind sent.
func protoKindToAPIType(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.StringKind,
		protoreflect.Int64Kind,
		protoreflect.Uint64Kind,
		protoreflect.Sint64Kind:
		return openapi3.TypeString
	case protoreflect.Int32Kind,
		protoreflect.Uint32Kind,
		protoreflect.Sint32Kind:
		return openapi3.TypeInteger
	case protoreflect.BoolKind:
		return openapi3.TypeBoolean
	case protoreflect.MessageKind:
		return openapi3.TypeObject
	default:
		return openapi3.TypeNumber
	}
}

// getPackageSchema returns the full schema reference from an entity name.
func (g *Generator) getPackageSchema(pkg, name string) string {
	prefix := "#/components/schemas"
	for _, pack := range g.packages {
		if strings.HasPrefix(name, pack) {
			return prefix + "." + name
		}
	}

	return path.Join(prefix, pkg+"."+name)
}

func setProperties(s *openapi3.Schema, fo *oapiv1.FieldOptions, requiredFn func()) error {
	s.Min = fo.Min
	s.Max = fo.Max

	if fo.MinLength != nil {
		s.MinLength = *fo.MinLength
	}

	s.MaxLength = fo.MaxLength

	if fo.MinItems != nil {
		s.MinItems = *fo.MinItems
	}

	s.MaxItems = fo.MaxItems

	if fo.UniqueItems != nil {
		s.UniqueItems = *fo.UniqueItems
	}

	if fo.MinProperties != nil {
		s.MinProps = *fo.MinProperties
	}

	s.MaxProps = fo.MaxProperties

	if fo.Pattern != nil {
		s.Pattern = *fo.Pattern
	}

	if fo.ExclusiveMin != nil {
		s.ExclusiveMin = *fo.ExclusiveMin
	}

	if fo.ExclusiveMax != nil {
		s.ExclusiveMax = *fo.ExclusiveMax
	}

	s.MultipleOf = fo.MultipleOf

	if len(fo.Enum) > 0 {
		s.Enum = make([]any, 0)

		for i := range fo.Enum {
			s.Enum = append(s.Enum, fo.Enum[i])
		}
	}

	if fo.Format != nil {
		s.Format = *fo.Format
	}

	if fo.AsType != nil {
		s.Type = *fo.AsType
	}

	if requiredFn != nil && fo.Required {
		requiredFn()
	}

	return nil
}

// setSchemaProperties sets properties on a property schema based on field options.
func (g *Generator) setSchemaProperties(s *openapi3.Schema, parent *openapi3.Schema, field *protogen.Field) error {
	extOptions := proto.GetExtension(field.Desc.Options(), oapiv1.E_Options)
	if extOptions == nil || extOptions == oapiv1.E_Options.InterfaceOf(oapiv1.E_Options.Zero()) {
		return nil
	}

	fo := extOptions.(*oapiv1.FieldOptions)

	return setProperties(s, fo, func() {
		parent.Required = append(parent.Required, g.getFieldName(field))
	})
}
