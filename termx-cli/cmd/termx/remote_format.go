package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func csvList(raw string) []string {
	return compactStringList(strings.Split(raw, ","))
}

func compactStringList(values []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func compactStringListOrEmpty(values []string) []string {
	out := compactStringList(values)
	if out == nil {
		return []string{}
	}
	return out
}

func printRemoteStatus(w io.Writer, status *remoteprotocol.Status) {
	if status == nil {
		return
	}
	fmt.Fprintf(w, "state:\t%s\n", status.State)
	fmt.Fprintf(w, "device_id:\t%s\n", status.DeviceID)
	fmt.Fprintf(w, "device_name:\t%s\n", status.DeviceName)
	fmt.Fprintf(w, "control_url:\t%s\n", status.ControlURL)
	fmt.Fprintf(w, "hub_url:\t%s\n", status.HubURL)
	fmt.Fprintf(w, "mode:\t%s\n", statusValue(status, "mode"))
	fmt.Fprintf(w, "allow_lan:\t%v\n", status.AllowLAN)
	fmt.Fprintf(w, "data_dir:\t%s\n", status.DataDir)
	fmt.Fprintf(w, "terminal_count:\t%d\n", status.TerminalCount)
	fmt.Fprintf(w, "updated_at:\t%s\n", status.UpdatedAt.Format(time.RFC3339))
	if status.Detail != "" {
		fmt.Fprintf(w, "detail:\t%s\n", status.Detail)
	}
}

func printRemoteLocalStatus(w io.Writer, status *remoteprotocol.LocalStatus) {
	if status == nil {
		return
	}
	fmt.Fprintf(w, "local_enabled:\t%t\n", status.Enabled)
	fmt.Fprintf(w, "local_web_url:\t%s\n", status.HTTPURL)
	fmt.Fprintf(w, "local_web_addr:\t%s\n", status.LocalWebAddr)
	fmt.Fprintf(w, "local_pair_url:\t%s\n", status.LocalPairURL)
	fmt.Fprintf(w, "ice_tcp_enabled:\t%t\n", status.ICETCPEnabled)
	fmt.Fprintf(w, "ice_tcp_addr:\t%s\n", status.ICETCPAddr)
	fmt.Fprintf(w, "ice_tcp_port:\t%d\n", status.ICETCPPort)
	fmt.Fprintf(w, "updated_at:\t%s\n", status.UpdatedAt.Format(time.RFC3339))
}

func buildRemotePairPayload(result *remoteprotocol.PairStartResult, _ *remoteprotocol.Status, hubURLs []string) map[string]any {
	payload := map[string]any{
		"type":           "termx_pair",
		"schema_version": 3,
		"machine": map[string]any{
			"id":   result.MachineID,
			"name": firstNonEmpty(result.MachineName, result.MachineID),
		},
		"addresses": map[string]any{
			"local":  []string{},
			"lan":    compactStringSlice(result.LocalPairURL),
			"public": compactStringListOrEmpty(hubURLs),
		},
		"pairing": map[string]any{
			"session_id":          result.PairSessionID,
			"secret":              result.PairSecret,
			"answer_proof_secret": result.AnswerProofSecret,
			"expires_at":          result.ExpiresAt.Format(time.RFC3339),
		},
	}
	cleanEmptyStrings(payload)
	return payload
}

func hubURLsForPairPayload(status *remoteprotocol.Status, override string) []string {
	if strings.TrimSpace(override) != "" {
		return compactStringList([]string{override})
	}
	if status == nil {
		return nil
	}
	hubURLs := compactStringList(status.HubURLs)
	if len(hubURLs) == 0 && strings.TrimSpace(status.HubURL) != "" {
		hubURLs = []string{strings.TrimSpace(status.HubURL)}
	}
	return hubURLs
}

func termxPairURI(payload map[string]any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "termx://pair?payload=" + base64.RawURLEncoding.EncodeToString(data), nil
}

func statusValue(status *remoteprotocol.Status, field string) string {
	if status == nil {
		return ""
	}
	switch field {
	case "control":
		return status.ControlURL
	case "hub":
		return status.HubURL
	case "mode":
		return status.Mode
	default:
		return ""
	}
}

func compactStringSlice(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func cleanEmptyStrings(value any) {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			switch typed := child.(type) {
			case string:
				if strings.TrimSpace(typed) == "" {
					delete(item, key)
				}
			case map[string]any:
				cleanEmptyStrings(typed)
			}
		}
	}
}
