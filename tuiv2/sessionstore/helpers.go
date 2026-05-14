package sessionstore

import (
	"strings"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/sessiondoc"
)

func EncodeSessionRecord(info SessionInfo, doc *sessiondoc.Doc) ([]byte, error) {
	return encodeSessionRecord(info, doc)
}

func DecodeSessionRecord(data []byte) (SessionInfo, *sessiondoc.Doc, error) {
	return decodeSessionRecord(data)
}

func SessionStateKey(sessionID string) string {
	return stateKey(sessionID)
}

func EventFromStorageChange(change protocol.StorageChangedData) (EventData, bool) {
	if change.AppID != AppID || change.Scope != storageScope {
		return EventData{}, false
	}
	sessionID, kind, ok := parseSessionKey(change.Key)
	if !ok {
		return EventData{}, false
	}
	return EventData{
		SessionID: sessionID,
		Revision:  change.Version,
		ViewID:    viewIDFromKey(change.Key),
		Deleted:   kind == "state" && strings.TrimSpace(change.Op) == storageOpDelete,
	}, true
}
