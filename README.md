# protoc-gen-openapi

![GitHub release (latest by date)](https://img.shields.io/github/v/release/dotjoshrc/protoc-gen-openapi?style=flat-square)
[![lint status](https://img.shields.io/github/actions/workflow/status/technicallyjosh/protoc-gen-openapi/run-lint.yaml?style=flat-square&label=lint)](https://github.com/dotjoshrc/protoc-gen-openapi/actions/workflows/run-lint.yaml)
[![go report card](https://goreportcard.com/badge/github.com/dotjoshrc/protoc-gen-openapi?style=flat-square)](https://goreportcard.com/report/github.com/technicallyjosh/protoc-gen-openapi)

**Yes**, this is _another_ protoc generator for OpenAPI. I created this for a
couple
reasons...

- I wanted to learn protoc generation with a real-world problem.
- The official google one sticks to gRPC and envoy standards. My team and I use
  Twirp and other REST frameworks. _Sometimes you just want to define models and
  an API for docs._
- Others try to do too much per the spec and fail to do the most common things
  well.

_**DISCLAIMER: This will be a limited subset of the OAPI3 specification. Not
everything will make it in here. Why? Read the last bullet point above. :)**_

_Some patterns were heavily inspired
by [gnostic](https://github.com/google/gnostic)._

## Installation

```terminal
go install github.com/technicallyjosh/protoc-gen-openapi@latest
```

## Options

| Option             | Description                                                                       | Default          |
|--------------------|-----------------------------------------------------------------------------------|------------------|
| `version`          | The version of the API.                                                           | 0.0.1            |
| `title`            | The title of the API.                                                             |                  |
| `description`      | A description of the API.                                                         |                  |
| `include`          | A list of proto package names to include only. `ignore` is ran after this         |                  |
| `ignore`           | A list of proto package names to ignore delimited by pipes.                       |                  |
| `default_response` | The default response to be used.<sup>1</sup>                                      |                  |
| `content_type`     | The content type to be associated with all operations.<sup>1</sup>                | application/json |
| `json_names`       | Use the JSON names that Protobuf provides. Otherwise, proto field names are used. | false            |
| `json_out`         | Create a JSON file instead of the default YAML.                                   | false            |
| `host`             | The host to be used for all operations.<sup>1</sup>                               |                  |
| `filename`         | Specify the filename to output.                                                   | openapi.yaml     |

<sup>1</sup> _Can be overridden on a file, service, or method._

## Build Examples

Below are some basic examples on how to use this generator.

### Protoc

```bash
protoc \
  --openapi_out=. \
  --openapi_opt=version=1.0.0 \
  --openapi_opt=title="My Awesome API" \
  api/some_service.proto
```

### Buf

**buf.yaml**

```yaml
# ... other things
deps:
  - buf.build/dotjoshrc/protoc-gen-openapi
```

**buf.gen.yaml**

```yaml
plugins:
  - name: go
    out: api
    opt:
      - paths=source_relative
  - name: openapi
    strategy: all # important so all files are ran in the same generation.
    out: api
    opt:
      - title=My Awesome API
      - description=Look how awesome my API is!
      - ignore=module.v1|module.v2
      - default_response=SomeErrorObject
```

## Basic Usage Example

```protobuf
syntax = "proto3";

import "oapi/v1/field.proto";
import "oapi/v1/file.proto";
import "oapi/v1/method.proto";
import "oapi/v1/service.proto";

option (oapi.v1.file) = {
  servers {
    url: "myawesomeapi.com"
  }

  security_schemes: {
    name: "bearer_auth"
    scheme: {
      type: "http"
      scheme: "bearer"
      bearer_format: "JWT"
    }
  }

  // Since this is at the file level, it's applied to all.
  security: {
    name: "bearer_auth"
    scopes: ["read:resource"]
  }
};

service MyService {
  option (oapi.v1.service) = {
    prefix: "/v1"
    x_display_name: "My Service"
  };

  rpc CreateSomething (CreateSomethingRequest) returns (CreateSomethingResponse) {
    option (oapi.v1.method) = {
      post: "create-something"
      summary: "Create Something"
      status: 201
    };
  }
}

message CreateSomethingRequest {
  // The name of something.
  // Example: something-awesome
  string name = 1 [(oapi.v1.required) = true];
}

message CreateSomethingResponse {
  // The ID of something.
  string id = 1;
  string name = 2;
}
```

## Features

> **Note**
>
> Defining features is a work in progress. I aim to explain all that's possible
> the best I can.

<details>
<summary><h3>Server definitions</h3></summary>

You can define servers at the file, service, or method level. Each one overrides
the previous. This allows for more advanced composition.

**Example:**

```protobuf
syntax = "proto3";

import "google/protobuf/empty.proto";
import "oapi/v1/file.proto";
import "oapi/v1/method.proto";
import "oapi/v1/service.proto";


option (oapi.v1.file) = {
  servers {
    url: "myawesomeapi.com" // file-defined for all services and methods
  }
};

service MyService {
  option (oapi.v1.service) = {
    servers {
      url: "myawesomeapi2.com" // overrides file-defined
    }
  };

  rpc CreateSomething (google.protobuf.Empty) returns (google.protobuf.Empty) {
    option (oapi.v1.method) = {
      servers {
        url: "myaweseomeapi3.com" // overrides service-defined
      }
    };
  }
}
```

</details>

<details>
<summary><h3>Service Prefixes</h3></summary>

Each service can have a path prefix set for all methods to inherit. This is
useful when versioning your API or if you have a parameter that is defined for
each method route.

_**You can override the entire path in the method by starting the path out with
a `/`.**_

**Example:**

```protobuf
syntax = "proto3";

import "google/protobuf/empty.proto";
import "oapi/v1/file.proto";
import "oapi/v1/method.proto";
import "oapi/v1/service.proto";

option (oapi.v1.file) = {
  servers {
    url: "myawesomeapi.com"
  }
};

service MyService {
  option (oapi.v1.service) = {
    prefix: "/v1"
  };

  rpc CreateSomething (google.protobuf.Empty) returns (google.protobuf.Empty) {
    option (oapi.v1.method) = {
      post: "create" // becomes /v1/create
    };
  }

  rpc OverrideSomething (google.protobuf.Empty) returns (google.protobuf.Empty) {
    option (oapi.v1.method) = {
      get: "/create" // becomes /create
    };
  }
}
```

</details>

## Features In Progress

- [Enum](https://json-schema.org/understanding-json-schema/reference/generic.html#enumerated-values)
  requirements on fields

## Contributing

Coming... Right now I prefer that it's just me until I get a solid hold on
generator patterns and the package is stable. I'm fully open to any suggestions
though!
