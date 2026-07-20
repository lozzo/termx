package edge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/control-plane/usage"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const relayUsageReportTimeout = 10 * time.Second

func (runtime *Runtime) runUsagePump(ctx context.Context) {
	defer runtime.usageWG.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		runtime.usageMu.Lock()
		if err := runtime.relay.FlushUsageOutbox(runtime.usageOutbox, ""); err == nil {
			_, _ = runtime.reportPendingUsage(ctx)
		}
		runtime.usageMu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (runtime *Runtime) reportPendingUsage(ctx context.Context) ([]*cloudpb.RelayUsageAck, error) {
	pending, err := runtime.usageOutbox.Pending()
	if err != nil || len(pending) == 0 {
		return nil, err
	}
	request := &cloudpb.ReportRelayUsageRequest{RelayId: runtime.config.Metadata.GetRelayId(), EdgeDeploymentId: runtime.config.Metadata.GetEdgeDeploymentId()}
	limit := len(pending)
	if limit > 128 {
		limit = 128
	}
	for _, record := range pending[:limit] {
		event, err := usage.ToProto(record.Event)
		if err != nil {
			return nil, err
		}
		request.Records = append(request.Records, &cloudpb.RelayUsageRecord{SignedLease: append([]byte(nil), record.SignedLease...), Event: event})
	}
	body, err := proto.Marshal(request)
	if err != nil {
		return nil, err
	}
	reportContext, cancel := context.WithTimeout(ctx, relayUsageReportTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(reportContext, http.MethodPost, runtime.config.ControllerURL+usage.InternalReportPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", httpapi.ProtobufMediaType)
	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(responseBody) == 0 || len(responseBody) > 4<<20 || response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != httpapi.ProtobufMediaType {
		return nil, errors.New("Controller Relay usage response is invalid")
	}
	acknowledged := &cloudpb.ReportRelayUsageResponse{}
	if proto.Unmarshal(responseBody, acknowledged) != nil || len(acknowledged.GetAcknowledgements()) != len(request.GetRecords()) {
		return nil, errors.New("Controller Relay usage acknowledgement is invalid")
	}
	for index, ack := range acknowledged.GetAcknowledgements() {
		record := pending[index]
		if ack.GetEventId() != record.Event.EventID || ack.GetSequence() != record.Event.Sequence {
			return nil, errors.New("Controller Relay usage acknowledgement is invalid")
		}
	}
	for _, ack := range acknowledged.GetAcknowledgements() {
		if err := runtime.usageOutbox.Ack(ack.GetEventId(), ack.GetSequence()); err != nil {
			return nil, err
		}
	}
	return acknowledged.GetAcknowledgements(), nil
}
