// Command generate_application_api validates the application command spec against
// the compiled protobuf descriptor and writes checked-in mechanical API glue.
package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/anytty/anytty/proto/apipb"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const defaultSpecPath = "proto/apipb/application_commands.csv"

var specHeader = []string{
	"command_oneof",
	"result_oneof",
	"controller_method",
	"capability",
	"validator_group",
	"response_kind",
	"terminal_response",
}

type commandSpec struct {
	CommandName      protoreflect.Name
	ResultName       protoreflect.Name
	ControllerMethod string
	Capability       protoreflect.Name
	ValidatorGroup   string
	ResponseKind     string
	TerminalResponse bool

	CommandField protoreflect.FieldDescriptor
	ResultField  protoreflect.FieldDescriptor
}

func main() {
	specPath := flag.String("spec", defaultSpecPath, "application command CSV spec")
	outRoot := flag.String("out-root", ".", "root directory for generated files")
	flag.Parse()

	specs, err := loadAndValidateSpec(*specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	outputs, err := generateApplicationAPI(specs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for name, content := range outputs {
		path := filepath.Join(*outRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "create generated directory: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func loadAndValidateSpec(path string) ([]commandSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open application command spec: %w", err)
	}
	defer file.Close()
	return parseAndValidateSpec(file)
}

func parseAndValidateSpec(reader io.Reader) ([]commandSpec, error) {
	records, err := csv.NewReader(reader).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read application command spec: %w", err)
	}
	if len(records) == 0 || !equalStrings(records[0], specHeader) {
		return nil, fmt.Errorf("application command spec header must be %q", strings.Join(specHeader, ","))
	}

	commandDescriptor := (&apipb.CommandEnvelope{}).ProtoReflect().Descriptor()
	resultDescriptor := (&apipb.ResultEnvelope{}).ProtoReflect().Descriptor()
	commandOneof := commandDescriptor.Oneofs().ByName("command")
	resultOneof := resultDescriptor.Oneofs().ByName("result")
	capabilities := apipb.ApiCapability(0).Descriptor().Values()
	if commandOneof == nil || resultOneof == nil {
		return nil, errors.New("compiled application protobuf descriptor is missing command/result oneofs")
	}

	seenCommands := make(map[protoreflect.Name]int, len(records)-1)
	seenMethods := make(map[string]int, len(records)-1)
	seenResults := make(map[protoreflect.Name]bool, resultOneof.Fields().Len())
	specs := make([]commandSpec, 0, len(records)-1)
	for index, record := range records[1:] {
		line := index + 2
		if len(record) != len(specHeader) {
			return nil, fmt.Errorf("application command spec line %d has %d columns, want %d", line, len(record), len(specHeader))
		}
		for column, value := range record {
			if value == "" {
				return nil, fmt.Errorf("application command spec line %d has empty %s", line, specHeader[column])
			}
			if strings.TrimSpace(value) != value {
				return nil, fmt.Errorf("application command spec line %d %s has surrounding whitespace", line, specHeader[column])
			}
		}

		spec := commandSpec{
			CommandName:      protoreflect.Name(record[0]),
			ResultName:       protoreflect.Name(record[1]),
			ControllerMethod: record[2],
			Capability:       protoreflect.Name(record[3]),
			ValidatorGroup:   record[4],
			ResponseKind:     record[5],
		}
		switch record[6] {
		case "true":
			spec.TerminalResponse = true
		case "false":
		default:
			return nil, fmt.Errorf("application command spec line %d terminal_response must be true or false", line)
		}
		if !validGoIdentifier(spec.ControllerMethod) || !unicode.IsUpper(rune(spec.ControllerMethod[0])) {
			return nil, fmt.Errorf("application command spec line %d controller_method %q is not an exported Go identifier", line, spec.ControllerMethod)
		}
		if previous := seenCommands[spec.CommandName]; previous != 0 {
			return nil, fmt.Errorf("application command spec line %d duplicates command %s from line %d", line, spec.CommandName, previous)
		}
		seenCommands[spec.CommandName] = line
		if previous := seenMethods[spec.ControllerMethod]; previous != 0 {
			return nil, fmt.Errorf("application command spec line %d duplicates controller method %s from line %d", line, spec.ControllerMethod, previous)
		}
		seenMethods[spec.ControllerMethod] = line

		spec.CommandField = commandOneof.Fields().ByName(spec.CommandName)
		if spec.CommandField == nil {
			return nil, fmt.Errorf("application command spec line %d names unknown command oneof %s", line, spec.CommandName)
		}
		spec.ResultField = resultOneof.Fields().ByName(spec.ResultName)
		if spec.ResultField == nil || spec.ResultName == "error" {
			return nil, fmt.Errorf("application command spec line %d names invalid result oneof %s", line, spec.ResultName)
		}
		if spec.CommandField.Kind() != protoreflect.MessageKind || spec.ResultField.Kind() != protoreflect.MessageKind {
			return nil, fmt.Errorf("application command spec line %d command/result fields must both be messages", line)
		}
		if !strings.HasSuffix(string(spec.CommandField.Message().Name()), "Command") || !strings.HasSuffix(string(spec.ResultField.Message().Name()), "Result") {
			return nil, fmt.Errorf("application command spec line %d command/result protobuf types are incoherent", line)
		}
		if capabilities.ByName(spec.Capability) == nil || spec.Capability == "API_CAPABILITY_UNSPECIFIED" {
			return nil, fmt.Errorf("application command spec line %d names unknown or unspecified capability %s", line, spec.Capability)
		}
		if !validValidatorGroup(spec.ValidatorGroup) {
			return nil, fmt.Errorf("application command spec line %d names unknown validator group %s", line, spec.ValidatorGroup)
		}
		if spec.ValidatorGroup == "operation" && (spec.CommandName != "cancel_operation" || spec.ControllerMethod != "CancelOperation") {
			return nil, fmt.Errorf("application command spec line %d operation validation is reserved for cancel_operation", line)
		}
		if spec.ValidatorGroup == "resource" && (spec.CommandName != "release_resource" || spec.ControllerMethod != "ReleaseResource") {
			return nil, fmt.Errorf("application command spec line %d resource validation is reserved for release_resource", line)
		}
		if spec.ValidatorGroup == "event" && (spec.CommandName != "event_subscribe" || spec.ControllerMethod != "EventSubscribe") {
			return nil, fmt.Errorf("application command spec line %d event stream handling is reserved for event_subscribe", line)
		}
		switch spec.ResponseKind {
		case "ack":
			if spec.ResultName != "acknowledge" || spec.TerminalResponse {
				return nil, fmt.Errorf("application command spec line %d ack response must use acknowledge without terminal response", line)
			}
		case "value":
			if spec.ResultName == "acknowledge" {
				return nil, fmt.Errorf("application command spec line %d value response cannot use acknowledge", line)
			}
		case "transaction":
			if spec.CommandName != "terminal_attach" || spec.ResultName != "terminal_attach" || spec.ControllerMethod != "TerminalAttach" || spec.TerminalResponse || spec.ValidatorGroup != "terminal" {
				return nil, fmt.Errorf("application command spec line %d transaction response is reserved for terminal_attach", line)
			}
		default:
			return nil, fmt.Errorf("application command spec line %d has unknown response kind %s", line, spec.ResponseKind)
		}
		seenResults[spec.ResultName] = true
		specs = append(specs, spec)
	}

	missingCommands := make([]string, 0)
	for index := 0; index < commandOneof.Fields().Len(); index++ {
		name := commandOneof.Fields().Get(index).Name()
		if seenCommands[name] == 0 {
			missingCommands = append(missingCommands, string(name))
		}
	}
	if len(missingCommands) != 0 || len(specs) != commandOneof.Fields().Len() {
		sort.Strings(missingCommands)
		return nil, fmt.Errorf("application command spec does not cover compiled command descriptor exactly once: rows=%d commands=%d missing=%v", len(specs), commandOneof.Fields().Len(), missingCommands)
	}
	missingResults := make([]string, 0)
	for index := 0; index < resultOneof.Fields().Len(); index++ {
		name := resultOneof.Fields().Get(index).Name()
		if name != "error" && !seenResults[name] {
			missingResults = append(missingResults, string(name))
		}
	}
	if len(missingResults) != 0 {
		sort.Strings(missingResults)
		return nil, fmt.Errorf("application command spec leaves result oneofs unused: %v", missingResults)
	}
	return specs, nil
}

func generateApplicationAPI(specs []commandSpec) (map[string][]byte, error) {
	generators := map[string]func([]commandSpec) []byte{
		"api_layer/application_commands.gen.go":      generateAPILayer,
		"api_mapping/application_commands.gen.go":    generateAPIMapping,
		"client/binding/application_commands.gen.go": generateClientBinding,
		"client/runtime/application_commands.gen.go": generateClientRuntime,
	}
	outputs := make(map[string][]byte, len(generators))
	for name, generator := range generators {
		formatted, err := format.Source(generator(specs))
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", name, err)
		}
		outputs[name] = formatted
	}
	return outputs, nil
}

func generateAPILayer(specs []commandSpec) []byte {
	var out bytes.Buffer
	writeGeneratedHeader(&out, "apilayer")
	out.WriteString("\nimport (\n\t\"context\"\n\n\tapimapping \"github.com/anytty/anytty/api_mapping\"\n\t\"github.com/anytty/anytty/proto/apipb\"\n)\n\n")
	out.WriteString("// TerminalController is the typed Proto boundary for terminal and path commands.\n")
	out.WriteString("type TerminalController interface {\n")
	for _, spec := range specs {
		if spec.ValidatorGroup == "terminal" {
			writeControllerMethod(&out, spec)
		}
	}
	out.WriteString("}\n\n")
	out.WriteString("// PlatformController is the typed Proto boundary for non-terminal application commands.\n")
	out.WriteString("type PlatformController interface {\n")
	for _, spec := range specs {
		if spec.ValidatorGroup != "terminal" && spec.ValidatorGroup != "operation" && spec.ValidatorGroup != "resource" {
			writeControllerMethod(&out, spec)
		}
	}
	out.WriteString("}\n\n")

	out.WriteString("func isTerminalCommand(command *apipb.CommandEnvelope) bool {\n\tswitch command.GetCommand().(type) {\n")
	writeGroupedCases(&out, specs, func(spec commandSpec) bool { return spec.ValidatorGroup == "terminal" })
	out.WriteString("\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n\n")

	out.WriteString("func validateApplicationCommand(command *apipb.CommandEnvelope) error {\n\tswitch command.GetCommand().(type) {\n")
	validators := []struct {
		group string
		call  string
	}{
		{"terminal", "apimapping.ValidateTerminalCommand(command)"},
		{"history_live", "apimapping.ValidateHistoryLiveCommand(command)"},
		{"event", "apimapping.ValidateEventSubscribeCommand(command)"},
		{"file_storage", "apimapping.ValidateFileStorageCommand(command)"},
		{"access_remote", "apimapping.ValidateAccessRemoteCommand(command)"},
	}
	for _, validator := range validators {
		writeGroupedCases(&out, specs, func(spec commandSpec) bool { return spec.ValidatorGroup == validator.group })
		fmt.Fprintf(&out, "\t\treturn %s\n", validator.call)
	}
	out.WriteString("\tdefault:\n\t\treturn nil\n\t}\n}\n\n")

	out.WriteString("func (service *Service) dispatchTerminalCommand(ctx context.Context, requestID string, session *apipb.EndpointSessionStamp, command *apipb.CommandEnvelope) *apipb.ResultEnvelope {\n\tswitch value := command.GetCommand().(type) {\n")
	for _, spec := range specs {
		if spec.ValidatorGroup != "terminal" {
			continue
		}
		writeDispatchCase(&out, spec, "service.terminals", true)
	}
	out.WriteString("\tdefault:\n\t\treturn errorResult(requestID, session, apimapping.ErrorToProto(&apimapping.ValidationError{Field: \"command\", Reason: \"unsupported terminal command\"}, false))\n\t}\n}\n\n")

	out.WriteString("func (service *Service) dispatchPlatformCommand(ctx context.Context, requestID string, session *apipb.EndpointSessionStamp, command *apipb.CommandEnvelope) *apipb.ResultEnvelope {\n\tswitch value := command.GetCommand().(type) {\n")
	for _, spec := range specs {
		if spec.ValidatorGroup == "terminal" || spec.ValidatorGroup == "operation" || spec.ValidatorGroup == "resource" {
			continue
		}
		writeDispatchCase(&out, spec, "service.platform", false)
	}
	out.WriteString("\tdefault:\n\t\treturn errorResult(requestID, session, apimapping.ErrorToProto(&apimapping.ValidationError{Field: \"command\", Reason: \"unsupported platform command\"}, false))\n\t}\n}\n")
	return out.Bytes()
}

func writeControllerMethod(out *bytes.Buffer, spec commandSpec) {
	commandType := spec.CommandField.Message().Name()
	switch spec.ResponseKind {
	case "ack":
		if spec.ValidatorGroup == "terminal" {
			fmt.Fprintf(out, "\t%s(context.Context, *apipb.EndpointSessionStamp, *apipb.%s) error\n", spec.ControllerMethod, commandType)
		} else {
			fmt.Fprintf(out, "\t%s(context.Context, *apipb.EndpointSessionStamp, *apipb.%s) (*apipb.AcknowledgeResult, error)\n", spec.ControllerMethod, commandType)
		}
	case "transaction":
		fmt.Fprintf(out, "\t%s(context.Context, *apipb.EndpointSessionStamp, *apipb.%s) (TerminalAttachTransaction, error)\n", spec.ControllerMethod, commandType)
	default:
		fmt.Fprintf(out, "\t%s(context.Context, *apipb.EndpointSessionStamp, *apipb.%s) (*apipb.%s, error)\n", spec.ControllerMethod, commandType, spec.ResultField.Message().Name())
	}
}

func writeGroupedCases(out *bytes.Buffer, specs []commandSpec, include func(commandSpec) bool) {
	matches := make([]commandSpec, 0, len(specs))
	for _, spec := range specs {
		if include(spec) {
			matches = append(matches, spec)
		}
	}
	for index, spec := range matches {
		prefix := "\tcase "
		if index != 0 {
			prefix = "\t\t"
		}
		fmt.Fprintf(out, "%s*apipb.CommandEnvelope_%s", prefix, goName(spec.CommandName))
		if index == len(matches)-1 {
			out.WriteString(":\n")
		} else {
			out.WriteString(",\n")
		}
	}
}

func writeDispatchCase(out *bytes.Buffer, spec commandSpec, controller string, terminal bool) {
	commandGoName := goName(spec.CommandName)
	resultGoName := goName(spec.ResultName)
	fmt.Fprintf(out, "\tcase *apipb.CommandEnvelope_%s:\n", commandGoName)
	switch spec.ResponseKind {
	case "ack":
		if terminal {
			fmt.Fprintf(out, "\t\terr := %s.%s(ctx, cloneSession(session), cloneMessage(value.%s))\n", controller, spec.ControllerMethod, commandGoName)
			out.WriteString("\t\tif err != nil {\n\t\t\treturn errorResult(requestID, session, apimapping.ErrorToProto(err, true))\n\t\t}\n\t\treturn acknowledge(requestID, session)\n")
		} else {
			fmt.Fprintf(out, "\t\tresult, err := %s.%s(ctx, cloneSession(session), cloneMessage(value.%s))\n", controller, spec.ControllerMethod, commandGoName)
			out.WriteString("\t\tif err != nil || result == nil {\n\t\t\treturn errorResult(requestID, session, apimapping.ErrorToProto(err, true))\n\t\t}\n")
			fmt.Fprintf(out, "\t\treturn &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_%s{%s: result}}\n", resultGoName, resultGoName)
		}
	case "transaction":
		fmt.Fprintf(out, "\t\ttransaction, err := %s.%s(ctx, cloneSession(session), cloneMessage(value.%s))\n", controller, spec.ControllerMethod, commandGoName)
		fmt.Fprintf(out, "\t\treturn service.terminalAttachResult(ctx, requestID, session, value.%s, transaction, err)\n", commandGoName)
	default:
		fmt.Fprintf(out, "\t\tresult, err := %s.%s(ctx, cloneSession(session), cloneMessage(value.%s))\n", controller, spec.ControllerMethod, commandGoName)
		if terminal {
			out.WriteString("\t\tif err != nil || result == nil {\n\t\t\treturn terminalResultError(requestID, session, err)\n\t\t}\n")
			fmt.Fprintf(out, "\t\treturn &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_%s{%s: cloneMessage(result)}}\n", resultGoName, resultGoName)
		} else {
			out.WriteString("\t\tif err != nil || result == nil {\n\t\t\treturn errorResult(requestID, session, apimapping.ErrorToProto(err, true))\n\t\t}\n")
			fmt.Fprintf(out, "\t\treturn &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_%s{%s: result}}\n", resultGoName, resultGoName)
		}
	}
}

func generateAPIMapping(specs []commandSpec) []byte {
	var out bytes.Buffer
	writeGeneratedHeader(&out, "apimapping")
	out.WriteString("\nimport \"github.com/anytty/anytty/proto/apipb\"\n\n")
	out.WriteString("// RequiredCapabilityForCommand returns the capability declared for a typed command.\n")
	out.WriteString("func RequiredCapabilityForCommand(command *apipb.CommandEnvelope) apipb.ApiCapability {\n\tif command == nil {\n\t\treturn apipb.ApiCapability_API_CAPABILITY_UNSPECIFIED\n\t}\n\tswitch command.GetCommand().(type) {\n")
	for _, spec := range specs {
		fmt.Fprintf(&out, "\tcase *apipb.CommandEnvelope_%s:\n\t\treturn apipb.ApiCapability_%s\n", goName(spec.CommandName), spec.Capability)
	}
	out.WriteString("\tdefault:\n\t\treturn apipb.ApiCapability_API_CAPABILITY_UNSPECIFIED\n\t}\n}\n")
	return out.Bytes()
}

func generateClientRuntime(specs []commandSpec) []byte {
	var out bytes.Buffer
	writeGeneratedHeader(&out, "runtime")
	out.WriteString("\nimport (\n\t\"context\"\n\n\t\"github.com/anytty/anytty/proto/apipb\"\n)\n\n")
	for _, spec := range specs {
		writeClientWrapper(&out, spec)
	}
	return out.Bytes()
}

func generateClientBinding(specs []commandSpec) []byte {
	var out bytes.Buffer
	writeGeneratedHeader(&out, "binding")
	out.WriteString("\nimport \"github.com/anytty/anytty/proto/apipb\"\n\n")
	out.WriteString("func requiresTerminalResponse(command *apipb.CommandEnvelope) bool {\n\tswitch command.GetCommand().(type) {\n")
	writeGroupedCases(&out, specs, func(spec commandSpec) bool { return spec.TerminalResponse })
	out.WriteString("\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n")
	return out.Bytes()
}

func writeClientWrapper(out *bytes.Buffer, spec commandSpec) {
	method := spec.ControllerMethod
	if method == "EventSubscribe" {
		method = "executeEventSubscribe"
	}
	commandGoName := goName(spec.CommandName)
	commandType := spec.CommandField.Message().Name()
	resultType := spec.ResultField.Message().Name()
	fmt.Fprintf(out, "// %s executes the %s application command.\n", method, spec.CommandName)
	if spec.ResponseKind == "ack" {
		fmt.Fprintf(out, "func (session *ApplicationSession) %s(ctx context.Context, command *apipb.%s) error {\n", method, commandType)
	} else {
		fmt.Fprintf(out, "func (session *ApplicationSession) %s(ctx context.Context, command *apipb.%s) (*apipb.%s, error) {\n", method, commandType, resultType)
	}
	fmt.Fprintf(out, "\tresult, err := session.Execute(ctx, &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_%s{%s: command}})\n", commandGoName, commandGoName)
	if spec.ResponseKind == "ack" {
		out.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n\tif result.GetAcknowledge() == nil {\n\t\treturn missingApplicationResult(\"acknowledge\")\n\t}\n\treturn nil\n}\n\n")
		return
	}
	resultGoName := goName(spec.ResultName)
	out.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	fmt.Fprintf(out, "\tif result.Get%s() == nil {\n\t\treturn nil, missingApplicationResult(%q)\n\t}\n", resultGoName, spec.ResultName)
	fmt.Fprintf(out, "\treturn result.Get%s(), nil\n}\n\n", resultGoName)
}

func writeGeneratedHeader(out *bytes.Buffer, packageName string) {
	out.WriteString("// Code generated by scripts/generate_application_api.go; DO NOT EDIT.\n\n")
	fmt.Fprintf(out, "package %s\n", packageName)
}

func goName(name protoreflect.Name) string {
	parts := strings.Split(string(name), "_")
	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func validGoIdentifier(value string) bool {
	for index, character := range value {
		if index == 0 && !unicode.IsLetter(character) && character != '_' {
			return false
		}
		if index > 0 && !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' {
			return false
		}
	}
	return value != ""
}

func validValidatorGroup(group string) bool {
	switch group {
	case "operation", "resource", "terminal", "history_live", "event", "file_storage", "access_remote":
		return true
	default:
		return false
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
