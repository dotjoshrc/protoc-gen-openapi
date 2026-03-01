package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/objx"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"

	oapiv1 "github.com/dotjoshrc/protoc-gen-openapi/internal/gen/go/oapi/v1"
	"github.com/dotjoshrc/protoc-gen-openapi/internal/generator/util"
)

// Config holds the configuration for the generator.
type Config struct {
	ContentType     *string
	DefaultResponse *string
	Description     *string
	Filename        *string
	Host            *string
	Ignore          *string
	Include         *string
	JSONOutput      *bool
	Title           *string
	UseJSONNames    *bool
	Version         *string
}

// Generator is an instance that parses the given folder and its Protobuf files into OAPI.
type Generator struct {
	config   Config
	plugin   *protogen.Plugin
	packages []string
}

// New creates and returns a new Generator instance.
func New(plugin *protogen.Plugin, conf Config) *Generator {
	return &Generator{
		config:   conf,
		plugin:   plugin,
		packages: make([]string, 0),
	}
}

// Run is the entrypoint method to generate OAPI from all Protobuf files. It builds the document and
// then generates the OAPI file.
func (g *Generator) Run() error {
	useJSON := *g.config.JSONOutput

	doc, err := g.buildDocument()
	if err != nil {
		return err
	}

	filename := *g.config.Filename + ".yaml"

	fileBuffer := bytes.Buffer{}
	jsonBytes, err := doc.MarshalJSON()
	if err != nil {
		return err
	}

	if useJSON {
		filename = *g.config.Filename + ".json"
		fileBuffer.Write(jsonBytes)
	} else {
		// Extra hops to get JSON to YAML.
		var i any
		err = json.Unmarshal(jsonBytes, &i)
		if err != nil {
			return err
		}

		encoder := yaml.NewEncoder(&fileBuffer)
		encoder.SetIndent(2)

		err = encoder.Encode(i)
		if err != nil {
			return err
		}
	}

	fileBytes := fileBuffer.Bytes()

	outFile := g.plugin.NewGeneratedFile(filename, "")

	patchedBytes, err := g.patchEmptySchemas(fileBytes)
	if err != nil {
		return err
	}

	patchedBytes, err = g.patchRemovedSecurity(patchedBytes)
	if err != nil {
		return err
	}

	_, err = outFile.Write(patchedBytes)

	return err
}

func (g *Generator) patchRemovedSecurity(fileBytes []byte) ([]byte, error) {
	data := make(map[string]any)

	useJSON := *g.config.JSONOutput

	var err error
	if useJSON {
		err = json.Unmarshal(fileBytes, &data)
	} else {
		err = yaml.Unmarshal(fileBytes, &data)
	}
	if err != nil {
		return nil, err
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	m, err := objx.FromJSON(string(jsonBytes))
	if err != nil {
		return nil, err
	}

	for pathKey := range m.Get("paths").ObjxMap() {
		pathPath := fmt.Sprintf("paths[%s]", pathKey)

		for methodKey := range m.Get(pathPath).ObjxMap() {
			methodPath := fmt.Sprintf("%s[%s]", pathPath, methodKey)
			securityPath := fmt.Sprintf("%s.security", methodPath)
			security := m.Get(securityPath)

			if security.IsObjxMapSlice() && len(security.ObjxMapSlice()) == 1 {
				// We need to look for single in array and with the name of
				// "___remove"
			outer:
				for _, obj := range security.ObjxMapSlice() {
					for key := range obj {
						if key == "___remove" {
							m.Set(securityPath, make([]string, 0))

							break outer
						}
					}
				}
			}
		}
	}

	if useJSON {
		return json.Marshal(m)
	}

	buffer := bytes.Buffer{}
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)

	err = encoder.Encode(m)

	return buffer.Bytes(), err
}

// patchEmptySchemas finds any schemas that are empty and updates them to have
// an empty `Properties` node.
func (g *Generator) patchEmptySchemas(fileBytes []byte) ([]byte, error) {
	data := make(map[string]any)

	useJSON := *g.config.JSONOutput

	var err error
	if useJSON {
		err = json.Unmarshal(fileBytes, &data)
	} else {
		err = yaml.Unmarshal(fileBytes, &data)
	}
	if err != nil {
		return nil, err
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	m, err := objx.FromJSON(string(jsonBytes))
	if err != nil {
		return nil, err
	}

	for pathKey := range m.Get("paths").ObjxMap() {
		pathPath := fmt.Sprintf("paths[%s]", pathKey)

		for methodKey := range m.Get(pathPath).ObjxMap() {
			methodPath := fmt.Sprintf("%s[%s]", pathPath, methodKey)

			schemaKey := fmt.Sprintf("%s.requestBody.content.application/json.schema", methodPath)
			schema := m.Get(schemaKey)

			if schema.IsObjxMap() && len(schema.ObjxMap()) == 0 {
				schema.ObjxMap().Set("properties", make(map[string]any))
			}

			for resKey := range m.Get(methodPath + ".responses").ObjxMap() {
				schemaKey := fmt.Sprintf("%s.responses.%s.content.application/json.schema", methodPath, resKey)
				schema := m.Get(schemaKey)
				if schema.IsObjxMap() && len(schema.ObjxMap()) == 0 {
					schema.ObjxMap().Set("properties", make(map[string]any))
				}
			}
		}
	}

	if useJSON {
		return json.Marshal(m)
	}

	buffer := bytes.Buffer{}
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)

	err = encoder.Encode(m)

	return buffer.Bytes(), err
}

// buildDocument builds out the base of the OAPI document with some defaults.
func (g *Generator) buildDocument() (*openapi3.T, error) {
	doc := &openapi3.T{
		Extensions: make(map[string]any),
		OpenAPI:    "3.0.3",
		Components: &openapi3.Components{
			SecuritySchemes: make(openapi3.SecuritySchemes),
			Schemas:         make(openapi3.Schemas),
			RequestBodies:   make(openapi3.RequestBodies),
			Responses:       openapi3.NewResponses(),
		},
		Info: &openapi3.Info{
			Title:       *g.config.Title,
			Description: *g.config.Description,
			Version:     *g.config.Version,
		},
		Paths:    make(openapi3.Paths),
		Security: make(openapi3.SecurityRequirements, 0),
		Servers:  make(openapi3.Servers, 0),
		Tags:     make(openapi3.Tags, 0),
	}

	included := strings.Split(*g.config.Include, "|")
	ignored := strings.Split(*g.config.Ignore, "|")

	files := g.plugin.Files

	if len(included) > 0 {
		files = filterIncludedFiles(g.plugin.Files, included)
	}

	if len(ignored) > 0 {
		if len(files) == 0 {
			files = g.plugin.Files
		}

		files = filterIgnoredFiles(files, ignored)
	}

	for _, file := range files {
		g.buildMessageMap(file.Messages)

		// We use the package name for fully qualified schema names.
		err := g.addSchemasToDoc(doc, file.Messages)
		if err != nil {
			return nil, err
		}

		// Capture all package names for later use.
		g.packages = append(g.packages, file.Proto.GetPackage())
	}

	err := g.addDefaultResponse(doc)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		// Add servers even if there isn't a service. (File-based)
		err = addFileServersToDoc(doc, file)
		if err != nil {
			return nil, err
		}

		err = addFileSecurityToDoc(doc, file)
		if err != nil {
			return nil, err
		}

		err = g.addPathsToDoc(doc, file.Services)
		if err != nil {
			return nil, err
		}
	}

	util.UniqueServers(doc)
	util.UniqueTags(doc)

	return doc, nil
}

func addFileServersToDoc(doc *openapi3.T, file *protogen.File) error {
	extFile := proto.GetExtension(file.Desc.Options(), oapiv1.E_File)
	if extFile != nil && extFile != oapiv1.E_File.InterfaceOf(oapiv1.E_File.Zero()) {
		fileOptions := extFile.(*oapiv1.FileOptions)

		if fileOptions.Host != "" {
			server, err := NewServer(fileOptions.Host)
			if err != nil {
				return err
			}
			doc.Servers = append(doc.Servers, server)
		}

		if len(fileOptions.Servers) > 0 {
			for _, fileServer := range fileOptions.Servers {
				server, err := NewServer(fileServer.Url)
				if err != nil {
					return err
				}
				doc.Servers = append(doc.Servers, server)
			}
		}
	}

	return nil
}

func addFileSecurityToDoc(doc *openapi3.T, file *protogen.File) error {
	extFile := proto.GetExtension(file.Desc.Options(), oapiv1.E_File)

	if extFile != nil && extFile != oapiv1.E_File.InterfaceOf(oapiv1.E_File.Zero()) {
		fileOptions := extFile.(*oapiv1.FileOptions)

		for _, scheme := range fileOptions.SecuritySchemes {
			doc.Components.SecuritySchemes[scheme.Name] = &openapi3.SecuritySchemeRef{
				Value: &openapi3.SecurityScheme{
					Type:             scheme.Scheme.Type,
					Description:      "",
					Name:             scheme.Scheme.Name,
					In:               scheme.Scheme.In,
					Scheme:           scheme.Scheme.Scheme,
					BearerFormat:     scheme.Scheme.BearerFormat,
					OpenIdConnectUrl: scheme.Scheme.OpenIdConnectUrl,
					// TODO: Implement flows
					Flows: nil,
				},
			}
		}

		requirements := make([]openapi3.SecurityRequirement, 0)
		for _, sec := range fileOptions.Security {
			requirements = append(requirements, openapi3.SecurityRequirement{
				sec.Name: sec.Scopes,
			})
		}

		doc.Security = requirements
	}

	return nil
}

func filterIgnoredFiles(allFiles []*protogen.File, ignored []string) []*protogen.File {
	files := make([]*protogen.File, 0)

Files:
	for _, file := range allFiles {
		if !file.Generate {
			continue
		}

		for _, ignoredPackage := range ignored {
			if file.Proto.GetPackage() == ignoredPackage {
				continue Files
			}
		}

		files = append(files, file)
	}

	return files
}

func filterIncludedFiles(allFiles []*protogen.File, included []string) []*protogen.File {
	files := make([]*protogen.File, 0)

Files:
	for _, file := range allFiles {
		if !file.Generate {
			continue
		}

		for _, ignoredPackage := range included {
			if file.Proto.GetPackage() != ignoredPackage {
				continue Files
			}
		}

		files = append(files, file)
	}

	return files
}
