package binding

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/proto/bindingpb"
	"google.golang.org/protobuf/proto"
)

// EngineCommand 解析 bindingpb.EngineCommand，并把命令路由到 engine-owned 异步 operation。
// C/JNI/WASM 只暴露这个通用 Proto 入口；业务命令类型不得继续扩张跨语言符号面。
func (engine *Engine) EngineCommand(payload []byte) (uint64, error) {
	if err := validatePayload(payload); err != nil {
		return 0, err
	}
	command := &bindingpb.EngineCommand{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, command); err != nil {
		return 0, fmt.Errorf("decode engine command: %w", err)
	}
	switch value := command.GetCommand().(type) {
	case *bindingpb.EngineCommand_ImportPairing:
		encoded, err := proto.Marshal(value.ImportPairing)
		if err != nil {
			return 0, err
		}
		return engine.ImportPairing(encoded)
	case *bindingpb.EngineCommand_DeleteCredential:
		encoded, err := proto.Marshal(value.DeleteCredential)
		if err != nil {
			return 0, err
		}
		return engine.DeleteCredential(encoded)
	case *bindingpb.EngineCommand_EndpointRegistryGet:
		return engine.startEndpointRegistryGet(value.EndpointRegistryGet)
	case *bindingpb.EngineCommand_EndpointUpsert:
		return engine.startEndpointUpsert(value.EndpointUpsert)
	case *bindingpb.EngineCommand_EndpointDelete:
		return engine.startEndpointDelete(value.EndpointDelete)
	default:
		return 0, fmt.Errorf("engine command is required")
	}
}

func (engine *Engine) registryHost() (EndpointRegistryHost, error) {
	host, ok := engine.host.(EndpointRegistryHost)
	if !ok {
		return nil, fmt.Errorf("binding host does not support endpoint registry operations")
	}
	return host, nil
}

func (engine *Engine) startEndpointRegistryGet(request *bindingpb.EndpointRegistryGetRequest) (uint64, error) {
	if request == nil || request.GetRequestId() == "" {
		return 0, fmt.Errorf("endpoint registry get request is incomplete")
	}
	host, err := engine.registryHost()
	if err != nil {
		return 0, err
	}
	handle, operationContext, err := engine.startOperation()
	if err != nil {
		return 0, err
	}
	go engine.runEndpointRegistryGet(handle, operationContext, host, proto.Clone(request).(*bindingpb.EndpointRegistryGetRequest))
	return handle, nil
}

func (engine *Engine) startEndpointUpsert(request *bindingpb.EndpointUpsertRequest) (uint64, error) {
	if request == nil || request.GetRequestId() == "" || request.GetEndpoint() == nil {
		return 0, fmt.Errorf("endpoint upsert request is incomplete")
	}
	host, err := engine.registryHost()
	if err != nil {
		return 0, err
	}
	handle, operationContext, err := engine.startOperation()
	if err != nil {
		return 0, err
	}
	go engine.runEndpointUpsert(handle, operationContext, host, proto.Clone(request).(*bindingpb.EndpointUpsertRequest))
	return handle, nil
}

func (engine *Engine) startEndpointDelete(request *bindingpb.EndpointDeleteRequest) (uint64, error) {
	if request == nil || request.GetRequestId() == "" || request.GetEndpointId() == "" {
		return 0, fmt.Errorf("endpoint delete request is incomplete")
	}
	host, err := engine.registryHost()
	if err != nil {
		return 0, err
	}
	handle, operationContext, err := engine.startOperation()
	if err != nil {
		return 0, err
	}
	go engine.runEndpointDelete(handle, operationContext, host, proto.Clone(request).(*bindingpb.EndpointDeleteRequest))
	return handle, nil
}

// ImportPairing 解析 bindingpb.ImportPairingRequest 并异步请求可选 PairingHost。
// 完成结果只通过 NextEvent 发布；binding 不解析二维码、ticket 或 credential 内容。
func (engine *Engine) ImportPairing(payload []byte) (uint64, error) {
	if err := validatePayload(payload); err != nil {
		return 0, err
	}
	request := &bindingpb.ImportPairingRequest{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, request); err != nil {
		return 0, fmt.Errorf("decode import pairing request: %w", err)
	}
	if request.GetRequestId() == "" || request.GetPortablePayload() == "" {
		return 0, fmt.Errorf("import pairing request is incomplete")
	}
	host, ok := engine.host.(PairingHost)
	if !ok {
		return 0, fmt.Errorf("binding host does not support pairing import")
	}
	handle, operationContext, err := engine.startOperation()
	if err != nil {
		return 0, err
	}
	go engine.runImportPairing(handle, operationContext, host, proto.Clone(request).(*bindingpb.ImportPairingRequest))
	return handle, nil
}

// DeleteCredential 解析 bindingpb.DeleteCredentialRequest 并异步删除平台 credential。
// 它不撤销 daemon grant；结果只通过 NextEvent 发布并需要显式 Release operation handle。
func (engine *Engine) DeleteCredential(payload []byte) (uint64, error) {
	if err := validatePayload(payload); err != nil {
		return 0, err
	}
	request := &bindingpb.DeleteCredentialRequest{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, request); err != nil {
		return 0, fmt.Errorf("decode delete credential request: %w", err)
	}
	if request.GetRequestId() == "" || request.GetCredentialRef() == "" {
		return 0, fmt.Errorf("delete credential request is incomplete")
	}
	host, ok := engine.host.(CredentialHost)
	if !ok {
		return 0, fmt.Errorf("binding host does not support credential deletion")
	}
	handle, operationContext, err := engine.startOperation()
	if err != nil {
		return 0, err
	}
	go engine.runDeleteCredential(handle, operationContext, host, proto.Clone(request).(*bindingpb.DeleteCredentialRequest))
	return handle, nil
}

func (engine *Engine) runImportPairing(handle uint64, ctx context.Context, host PairingHost, request *bindingpb.ImportPairingRequest) {
	result, err := host.ImportPairing(ctx, request)
	if ctx.Err() != nil {
		result = nil
		err = ctx.Err()
	}
	engine.markOperationDone(handle)
	if result == nil {
		result = &bindingpb.ImportPairingResult{}
	} else {
		result = proto.Clone(result).(*bindingpb.ImportPairingResult)
	}
	result.RequestId = request.GetRequestId()
	result.OperationHandle = handle
	if err != nil {
		result.Error = apiError(err)
	}
	engine.emit(&bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_ImportPairing{ImportPairing: result}})
}

func (engine *Engine) runDeleteCredential(handle uint64, ctx context.Context, host CredentialHost, request *bindingpb.DeleteCredentialRequest) {
	err := host.DeleteCredential(ctx, request)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	engine.markOperationDone(handle)
	result := &bindingpb.DeleteCredentialResult{RequestId: request.GetRequestId(), OperationHandle: handle}
	if err != nil {
		result.Error = apiError(err)
	}
	engine.emit(&bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_DeleteCredential{DeleteCredential: result}})
}

func (engine *Engine) runEndpointRegistryGet(handle uint64, ctx context.Context, host EndpointRegistryHost, request *bindingpb.EndpointRegistryGetRequest) {
	result, err := host.GetEndpointRegistry(ctx, request)
	if ctx.Err() != nil {
		result, err = nil, ctx.Err()
	}
	engine.markOperationDone(handle)
	if result == nil {
		result = &bindingpb.EndpointRegistryGetResult{}
	} else {
		result = proto.Clone(result).(*bindingpb.EndpointRegistryGetResult)
	}
	result.RequestId, result.OperationHandle = request.GetRequestId(), handle
	if err != nil {
		result.Error = apiError(err)
	}
	engine.emit(&bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_EndpointRegistryGet{EndpointRegistryGet: result}})
}

func (engine *Engine) runEndpointUpsert(handle uint64, ctx context.Context, host EndpointRegistryHost, request *bindingpb.EndpointUpsertRequest) {
	result, err := host.UpsertEndpoint(ctx, request)
	if ctx.Err() != nil {
		result, err = nil, ctx.Err()
	}
	engine.markOperationDone(handle)
	if result == nil {
		result = &bindingpb.EndpointUpsertResult{}
	} else {
		result = proto.Clone(result).(*bindingpb.EndpointUpsertResult)
	}
	result.RequestId, result.OperationHandle = request.GetRequestId(), handle
	if err != nil {
		result.Error = apiError(err)
	}
	engine.emit(&bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_EndpointUpsert{EndpointUpsert: result}})
}

func (engine *Engine) runEndpointDelete(handle uint64, ctx context.Context, host EndpointRegistryHost, request *bindingpb.EndpointDeleteRequest) {
	result, err := host.DeleteEndpoint(ctx, request)
	if ctx.Err() != nil {
		result, err = nil, ctx.Err()
	}
	engine.markOperationDone(handle)
	if result == nil {
		result = &bindingpb.EndpointDeleteResult{}
	} else {
		result = proto.Clone(result).(*bindingpb.EndpointDeleteResult)
	}
	result.RequestId, result.OperationHandle = request.GetRequestId(), handle
	if err != nil {
		result.Error = apiError(err)
	}
	engine.emit(&bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_EndpointDelete{EndpointDelete: result}})
}
