package introspection_datasource

import (
	"bytes"
	"strconv"
)

type requestType int

const (
	SchemaRequestType requestType = iota + 1
	TypeRequestType
)

const (
	schemaFieldName = "__schema"
	typeFieldName   = "__type"
)

type introspectionInput struct {
	RequestType requestType `json:"request_type"`
	TypeName    *string     `json:"type_name"`
}

var (
	lBrace           = []byte("{")
	rBrace           = []byte("}")
	comma            = []byte(",")
	requestTypeField = []byte(`"request_type":`)
	typeNameField    = []byte(`"type_name":"{{ .arguments.name }}"`)
)

func buildInput(fieldName string, hasIncludeDeprecatedArgument bool) string {
	buf := &bytes.Buffer{}
	buf.Write(lBrace)

	switch fieldName {
	case typeFieldName:
		writeRequestTypeField(buf, TypeRequestType)
		buf.Write(comma)
		buf.Write(typeNameField)
	default:
		writeRequestTypeField(buf, SchemaRequestType)
	}

	buf.Write(rBrace)

	return buf.String()
}

func writeRequestTypeField(buf *bytes.Buffer, inputType requestType) {
	buf.Write(requestTypeField)
	buf.Write([]byte(strconv.Itoa(int(inputType))))
}
