package generator

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"

	oapiv1 "github.com/dotjoshrc/protoc-gen-openapi/gen/go/dotjoshrc/oapi/v1"
)

func NewServer(host string) (*openapi3.Server, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}

	if u.Scheme == "" {
		u.Scheme = "https"
	}

	server := &openapi3.Server{
		URL: u.String(),
	}

	return server, nil
}

// addPathsToDoc adds paths from services and methods to the OAPI doc. This includes all request and
// response bodies.
func (g *Generator) addPathsToDoc(doc *openapi3.T, services []*protogen.Service) error {
	// Default to config-defined.
	contentType := *g.config.ContentType
	host := *g.config.Host

	for _, service := range services {
		var pathPrefix string

		// Apply/Override file options.
		extFile := proto.GetExtension(service.Desc.ParentFile().Options(), oapiv1.E_File)
		if extFile != nil && extFile != oapiv1.E_File.InterfaceOf(oapiv1.E_File.Zero()) {
			fileOptions := extFile.(*oapiv1.FileOptions)
			host = fileOptions.Host
			pathPrefix = fileOptions.Prefix
		}

		tagName := string(service.Desc.FullName())
		packageName := string(service.Desc.ParentFile().Package())
		serviceOptions := new(oapiv1.ServiceOptions)

		// Service options.
		extService := proto.GetExtension(service.Desc.Options(), oapiv1.E_Service)
		if extService != nil && extService != oapiv1.E_Service.InterfaceOf(oapiv1.E_Service.Zero()) {
			serviceOptions = extService.(*oapiv1.ServiceOptions)
		}

		security := make([]*oapiv1.Security, 0)

		var servers openapi3.Servers

		if serviceOptions.Host != "" {
			// Use service defined host.
			host = serviceOptions.Host

			server, err := NewServer(serviceOptions.Host)
			if err != nil {
				return err
			}

			doc.Servers = append(doc.Servers, server)
			servers = append(servers, server)
		}

		if len(serviceOptions.Servers) > 0 {
			for _, serviceServer := range serviceOptions.Servers {
				server, err := NewServer(serviceServer.Url)
				if err != nil {
					return err
				}

				servers = append(servers, server)
			}
		}

		if len(serviceOptions.AddServers) > 0 {
			if len(servers) == 0 {
				// If there are no servers defined on the service level, but we
				// have servers being added, we need to ensure both are on each
				// method.
				servers = append(servers, doc.Servers...)
			}

			for _, addServer := range serviceOptions.AddServers {
				server, err := NewServer(addServer.Url)
				if err != nil {
					return err
				}

				servers = append(servers, server)
			}
		}

		if serviceOptions.Prefix != "" {
			// Use service defined prefix.
			pathPrefix = serviceOptions.Prefix
		}

		if serviceOptions.ContentType != "" {
			// Use service defined content type.
			contentType = serviceOptions.ContentType
		}

		if len(serviceOptions.Security) > 0 {
			// Use service defined security.
			for _, s := range serviceOptions.Security {
				// If one is empty, this is how we'll clear it on an override.
				// e.g. security: {}
				if s.Name == "" {
					security = make([]*oapiv1.Security, 0)

					break
				}

				security = append(security, s)
			}
		}

		serviceDescription := g.parseComments(service.Comments.Leading).Description

		doc.Tags = append(doc.Tags, &openapi3.Tag{
			Name:        tagName,
			Description: serviceDescription,
			Extensions: map[string]any{
				"x-displayName": serviceOptions.XDisplayName,
			},
		})

		tagGroup := strings.TrimSpace(serviceOptions.XTagGroup)
		if tagGroup != "" {
			err := addTagGroup(doc, tagGroup, tagName)
			if err != nil {
				return err
			}
		}

		parameters, err := g.createParameters(
			pathPrefix,
			serviceOptions.PathParameter,
			serviceOptions.QueryParameter,
			serviceOptions.HeaderParameter,
			serviceOptions.CookieParameter,
		)
		if err != nil {
			return err
		}

		for _, method := range service.Methods {
			err := g.addOperation(addOperationParams{
				doc:               doc,
				service:           service,
				serviceOptions:    serviceOptions,
				method:            method,
				host:              host,
				contentType:       contentType,
				tagName:           tagName,
				pathPrefix:        pathPrefix,
				packageName:       packageName,
				serviceParameters: parameters,
				security:          security,
				servers:           servers,
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (g *Generator) createParameters(path string, pathParams, queryParams, headerParams, cookieParams []*oapiv1.Parameter) (openapi3.Parameters, error) {
	parameters, err := g.parseParameters(openapi3.ParameterInPath, path, pathParams)
	if err != nil {
		return nil, err
	}

	queryParameters, err := g.parseParameters(openapi3.ParameterInQuery, "", queryParams)
	if err != nil {
		return nil, err
	}
	parameters = append(parameters, queryParameters...)

	headerParameters, err := g.parseParameters(openapi3.ParameterInHeader, "", headerParams)
	if err != nil {
		return nil, err
	}
	parameters = append(parameters, headerParameters...)

	cookieParameters, err := g.parseParameters(openapi3.ParameterInCookie, "", cookieParams)
	if err != nil {
		return nil, err
	}
	parameters = append(parameters, cookieParameters...)

	return parameters, nil
}

// parseParameters parses and returns openapi3 converted parameters from defined parameters.
func (g *Generator) parseParameters(in, path string, parameters []*oapiv1.Parameter) (openapi3.Parameters, error) {
	params := make(openapi3.Parameters, 0)

	for _, parameter := range parameters {
		if in == openapi3.ParameterInPath && !strings.Contains(path, fmt.Sprintf("{%s}", parameter.Name)) {
			return nil, fmt.Errorf("parameter {%s} is missing from path %s", parameter.Name, path)
		}

		paramRef := &openapi3.ParameterRef{
			Value: &openapi3.Parameter{
				Name:     parameter.Name,
				In:       in,
				Required: in == openapi3.ParameterInPath,
			},
		}

		var paramType string

		switch parameter.Type {
		case oapiv1.Parameter_TYPE_UNSPECIFIED, oapiv1.Parameter_TYPE_STRING:
			paramType = openapi3.TypeString
		case oapiv1.Parameter_TYPE_INTEGER:
			paramType = openapi3.TypeInteger
		case oapiv1.Parameter_TYPE_NUMBER:
			paramType = openapi3.TypeNumber
		case oapiv1.Parameter_TYPE_BOOLEAN:
			paramType = openapi3.TypeBoolean
		default:
			return nil, fmt.Errorf("invalid parameter type: %s", parameter.Type)
		}

		paramRef.Value.Schema = &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				Type: paramType,
			},
		}

		if strings.TrimSpace(parameter.Description) != "" {
			paramRef.Value.Description = parameter.Description
		}

		if strings.TrimSpace(parameter.Example) != "" {
			paramRef.Value.Example = parameter.Example
		}

		if parameter.Required != nil {
			paramRef.Value.Required = *parameter.Required
		}

		if parameter.Options != nil {
			err := setProperties(paramRef.Value.Schema.Value, parameter.Options, func() {
				paramRef.Value.Required = true
			})
			if err != nil {
				return nil, err
			}
		}

		params = append(params, paramRef)
	}

	return params, nil
}

type addOperationParams struct {
	doc               *openapi3.T
	service           *protogen.Service
	method            *protogen.Method
	serviceOptions    *oapiv1.ServiceOptions
	host              string
	servers           openapi3.Servers
	contentType       string
	tagName           string
	pathPrefix        string
	packageName       string
	serviceParameters openapi3.Parameters
	security          []*oapiv1.Security
}

// addOperation creates an operation for a path and adds it.
// TODO: Break into smaller bits.
func (g *Generator) addOperation(p addOperationParams) error {
	servers := p.servers
	contentType := p.contentType

	operationID := string(p.service.Desc.Name() + "_" + p.method.Desc.Name())
	description := g.parseComments(p.method.Comments.Leading).Description

	var methodOptions *oapiv1.MethodOptions

	// Method options. If not present, continue to next.
	extMethod := proto.GetExtension(p.method.Desc.Options(), oapiv1.E_Method)
	if extMethod != nil && extMethod != oapiv1.E_Method.InterfaceOf(oapiv1.E_Method.Zero()) {
		methodOptions = extMethod.(*oapiv1.MethodOptions)
	} else {
		return nil
	}

	if methodOptions.Host != "" {
		server, err := NewServer(methodOptions.Host)
		if err != nil {
			return err
		}
		p.doc.Servers = append(p.doc.Servers, server)
		servers = append(servers, server)
	}

	if len(methodOptions.Servers) > 0 {
		// If defined at the method level, delete what came from service.
		servers = openapi3.Servers{}
		for _, methodServer := range methodOptions.Servers {
			server, err := NewServer(methodServer.Url)
			if err != nil {
				return err
			}

			servers = append(servers, server)
		}
	}

	if len(methodOptions.AddServers) > 0 {
		for _, addServer := range methodOptions.AddServers {
			server, err := NewServer(addServer.Url)
			if err != nil {
				return err
			}

			servers = append(servers, server)
		}
	}

	if methodOptions.ContentType != "" {
		// Use method defined content type.
		contentType = methodOptions.ContentType
	}

	if len(methodOptions.Security) > 0 {
		// Use method defined security.
		for _, s := range methodOptions.Security {
			// If one is empty, this is how we'll clear it on an override.
			// e.g. security: {}
			if s.Name == "" {
				p.security = []*oapiv1.Security{
					{
						Name: "___remove",
					},
				}

				break
			}

			p.security = append(p.security, s)
		}
	}

	var methodPath, methodName string

	op := &openapi3.Operation{
		Tags:        []string{p.tagName},
		Description: description,
		OperationID: operationID,
		Servers:     &servers,
		Deprecated:  methodOptions.Deprecated,
		Responses:   make(openapi3.Responses),
		Summary:     methodOptions.Summary,
		Parameters:  make(openapi3.Parameters, 0),
	}

	if len(p.security) > 0 {
		op.Security = new(openapi3.SecurityRequirements)
		for _, sec := range p.security {
			req := openapi3.SecurityRequirement{
				sec.Name: make([]string, 0),
			}

			if len(sec.Scopes) > 0 {
				req[sec.Name] = sec.Scopes
			}

			op.Security = op.Security.With(req)
		}
	}

	switch m := methodOptions.Method.(type) {
	case *oapiv1.MethodOptions_Get:
		methodName = http.MethodGet
		methodPath = m.Get
	case *oapiv1.MethodOptions_Put:
		methodName = http.MethodPut
		methodPath = m.Put
	case *oapiv1.MethodOptions_Post:
		methodName = http.MethodPost
		methodPath = m.Post
	case *oapiv1.MethodOptions_Delete:
		methodName = http.MethodDelete
		methodPath = m.Delete
	case *oapiv1.MethodOptions_Patch:
		methodName = http.MethodPatch
		methodPath = m.Patch
	default:
		return fmt.Errorf("method '%s' is missing a method", p.method.Desc.FullName())
	}

	// If the method's path starts with a "/", don't append the prefix from the service.
	if !strings.HasPrefix(methodPath, "/") {
		methodPath = path.Join(p.pathPrefix, methodPath)
	}

	var defaultResponseDesc string

	// Set the default response from the method or service if defined. Otherwise, use the
	// globally set one.
	if methodOptions.DefaultResponse != "" {
		// Use the method default response.
		schemaName := g.getPackageSchema(p.packageName, methodOptions.DefaultResponse)
		_, ok := p.doc.Components.Schemas[schemaName]
		if !ok {
			return fmt.Errorf("schema '%s' for method '%s' default response not found", schemaName, p.method.Desc.FullName())
		}

		op.Responses["default"] = &openapi3.ResponseRef{
			Value: &openapi3.Response{
				Description: &defaultResponseDesc,
				Content: openapi3.Content{
					contentType: &openapi3.MediaType{
						Schema: &openapi3.SchemaRef{
							Ref: newSchemaRef(schemaName),
						},
					},
				},
			},
		}
	} else if p.serviceOptions.DefaultResponse != "" {
		// Use the service default response
		schemaName := g.getPackageSchema(p.packageName, p.serviceOptions.DefaultResponse)
		_, ok := p.doc.Components.Schemas[schemaName]
		if !ok {
			return fmt.Errorf("schema '%s' for service '%s' default response not found", schemaName, p.service.Desc.FullName())
		}

		op.Responses["default"] = &openapi3.ResponseRef{
			Value: &openapi3.Response{
				Description: &defaultResponseDesc,
				Content: openapi3.Content{
					contentType: &openapi3.MediaType{
						Schema: &openapi3.SchemaRef{
							Ref: newSchemaRef(schemaName),
						},
					},
				},
			},
		}
	} else {
		// Use the global default response if available.
		if p.doc.Components.Responses.Default() != nil {
			op.Responses["default"] = &openapi3.ResponseRef{
				Ref: newResponseRef("default"),
			}
		}
	}

	if methodOptions.Status == 0 {
		// Default to 200 OK.
		methodOptions.Status = http.StatusOK
	}

	requestContent := openapi3.Content{
		contentType: &openapi3.MediaType{
			Schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Properties: make(openapi3.Schemas),
				},
			},
		},
	}

	responseContent := openapi3.Content{
		contentType: &openapi3.MediaType{
			Schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Properties: make(openapi3.Schemas),
				},
			},
		},
	}

	methodParameters, err := g.createParameters(
		methodPath,
		methodOptions.PathParameter,
		methodOptions.QueryParameter,
		methodOptions.HeaderParameter,
		methodOptions.CookieParameter,
	)
	if err != nil {
		return err
	}

	for _, methodParam := range methodParameters {
		// For each method parameter, we'll search to see if it's already
		// defined on the service.
		for _, serviceParam := range p.serviceParameters {
			if serviceParam.Value.In == methodParam.Value.In && serviceParam.Value.Name == methodParam.Value.Name {
				return fmt.Errorf(
					"%s %s parameter '%s' is already defined in the service definition",
					string(p.method.Desc.FullName()),
					methodParam.Value.In,
					methodParam.Value.Name,
				)
			}
		}
	}

	op.Parameters = append(p.serviceParameters, methodParameters...)

	if methodName != http.MethodGet {
		inputFullName := string(p.method.Input.Desc.FullName())
		message := allMessages.Get(inputFullName)

		// If another type is defined such as google.protobuf.Any or
		// google.protobuf.Empty, for now we'll just exit.
		// TODO: support `Any` type for requests 🤔
		if message != nil {
			requestSchemaRef := &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Properties: make(openapi3.Schemas),
				},
			}

			err := g.buildSchema(p.doc, message, requestSchemaRef)
			if err != nil {
				return err
			}

			requestContent.Get(contentType).Schema = requestSchemaRef

			op.RequestBody = &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Content: requestContent,
				},
			}
		}
	}

	outputFullName := string(p.method.Output.Desc.FullName())
	message := allMessages.Get(outputFullName)
	responseSchema := &openapi3.Schema{
		Properties: make(openapi3.Schemas),
	}
	err = g.buildSchema(p.doc, message, responseSchema.NewRef())
	if err != nil {
		return err
	}
	responseContent.Get(contentType).Schema = responseSchema.NewRef()

	responseCode := fmt.Sprintf("%d", methodOptions.Status)
	var responseDescription string
	op.Responses[responseCode] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Content:     responseContent,
			Description: &responseDescription,
		},
	}

	// Check for an existing path an append if it exists.
	existingPath := p.doc.Paths.Find(methodPath)
	if existingPath == nil {
		pathItem := new(openapi3.PathItem)
		pathItem.SetOperation(methodName, op)

		p.doc.Paths[methodPath] = pathItem
	} else {
		if existingPath.GetOperation(methodName) != nil {
			return fmt.Errorf("duplicate method '%s' for path '%s'", methodName, methodPath)
		}

		// append the method
		existingPath.SetOperation(methodName, op)
	}

	return nil
}
