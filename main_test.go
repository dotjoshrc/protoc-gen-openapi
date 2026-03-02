package main_test

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	jd "github.com/josephburnett/jd/lib"
	"github.com/stretchr/testify/suite"
	"gopkg.in/yaml.v3"
)

type TestOptions struct {
	defaultResponse string
	description     string
	testProto       string
	title           string
	version         string
	include         string
	ignore          string
}

type TestSuite struct {
	suite.Suite
	rawDoc  []byte
	options TestOptions
}

func (s *TestSuite) YAMLEqual(expected any, actual any, msgAndArgs ...any) {
	var expectedYAML, actualYAML string

	switch v := expected.(type) {
	case string:
		expectedYAML = v
	case []byte:
		expectedYAML = string(v)
	default:
		expectedBytes, err := yaml.Marshal(v)
		if err != nil {
			s.FailNow(fmt.Sprintf("failed to marshal expected: %#v", v), msgAndArgs...)
		}
		expectedYAML = string(expectedBytes)
	}

	switch v := actual.(type) {
	case string:
		actualYAML = v
	case []byte:
		actualYAML = string(v)
	default:
		actualBytes, err := yaml.Marshal(v)
		if err != nil {
			s.FailNow(fmt.Sprintf("failed to marshal actual: %#v", v), msgAndArgs...)
		}
		actualYAML = string(actualBytes)
	}

	expectedNode, _ := jd.ReadYamlString(expectedYAML)
	actualNode, _ := jd.ReadYamlString(actualYAML)

	diff := expectedNode.Diff(actualNode, jd.SET).Render(jd.COLOR)

	if diff != "" {
		s.Fail(fmt.Sprintf("Not Equal:\nexpected:\n%s\nactual:\n%s\n\n%s", expectedYAML, actualYAML, diff), msgAndArgs...)
	}
}

func (s *TestSuite) SetupSuite() {
	s.options = TestOptions{
		defaultResponse: "test.api.Error",
		description:     "test description",
		testProto:       "basic_test.proto",
		title:           "test title",
		version:         "1.1.0",
	}
}

func (s *TestSuite) BeforeTest(_, name string) {
	var filename string

	switch name {
	case "TestBasic":
		filename = "basic_test.proto"
	case "TestFile":
		filename = "file_test.proto"
	case "TestService":
		filename = "service_test.proto"
	case "TestMethod":
		filename = "method_test.proto"
	case "TestField":
		filename = "field_test.proto"
	default:
		s.FailNow("invalid test name")
	}

	err := exec.Command("rm", "-f", "test/openapi.yaml").Run()
	if err != nil {
		s.FailNow(err.Error())
	}

	out, err := exec.Command("protoc",
		"-I=proto/openapi",
		"-I=test",
		"--openapi_out=test",
		"--openapi_opt=version="+s.options.version,
		"--openapi_opt=title="+s.options.title,
		"--openapi_opt=description="+s.options.description,
		"--openapi_opt=default_response="+s.options.defaultResponse,
		"--openapi_opt=include="+s.options.include,
		"--openapi_opt=ignore="+s.options.ignore,
		"test/"+filename,
	).CombinedOutput()
	if err != nil {
		s.FailNow(string(out) + "\n" + err.Error())
	}

	s.rawDoc, err = os.ReadFile("test/openapi.yaml")
	if err != nil {
		s.FailNow(err.Error())
	}
}

func (s *TestSuite) TestBasic() {
	s.YAMLEqual(readFile("basic_test_openapi.yaml"), string(s.rawDoc))
}

func (s *TestSuite) TestFile() {
	s.YAMLEqual(readFile("file_test_openapi.yaml"), string(s.rawDoc))
}

func (s *TestSuite) TestService() {
	s.YAMLEqual(readFile("service_test_openapi.yaml"), string(s.rawDoc))
}

func (s *TestSuite) TestMethod() {
	s.YAMLEqual(readFile("method_test_openapi.yaml"), string(s.rawDoc))
}

func (s *TestSuite) TestField() {
	s.YAMLEqual(readFile("field_test_openapi.yaml"), string(s.rawDoc))
}

func TestSuites(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func readFile(name string) string {
	data, _ := os.ReadFile("test/" + name)

	return string(data)
}
