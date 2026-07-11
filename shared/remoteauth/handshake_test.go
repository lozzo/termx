package remoteauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/remoteauthpb"
	"github.com/lozzow/termx/shared/transport"
	"github.com/lozzow/termx/shared/transport/memory"
)

func TestHandshakeAuthenticatesAndSwitchesToTermxProtocol(t *testing.T) {
	identity, grant, now := handshakeFixture(t, Scope{TerminalID: "term-1"})
	clientConn, serverConn := memory.NewPair()
	defer clientConn.Close()
	defer serverConn.Close()
	type serverResult struct {
		claims Claims
		frame  []byte
		err    error
	}
	serverDone := make(chan serverResult, 1)
	go func() {
		claims, err := (ServerHandshake{Identity: identity, Now: fixedNow(now)}).Accept(context.Background(), serverConn, fixtureDTLSFingerprint(0x11))
		if err != nil {
			serverDone <- serverResult{err: err}
			return
		}
		frame, err := serverConn.Recv()
		serverDone <- serverResult{claims: claims, frame: frame, err: err}
	}()
	clientClaims, err := (ClientHandshake{Now: fixedNow(now)}).Authenticate(context.Background(), clientConn, ClientHandshakeRequest{
		ExpectedDeviceID:                 identity.DeviceID,
		ExpectedDeviceFingerprint:        identity.Fingerprint,
		CapabilityGrant:                  grant,
		DaemonDTLSCertificateFingerprint: fixtureDTLSFingerprint(0x11),
	})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if clientClaims.Scope.TerminalID != "term-1" {
		t.Fatalf("client claims = %+v", clientClaims)
	}
	wantFrame := []byte{0, 0, 0, 0, 0, 0, 0}
	if err := clientConn.Send(wantFrame); err != nil {
		t.Fatalf("send first termx frame: %v", err)
	}
	result := <-serverDone
	if result.err != nil {
		t.Fatalf("server Accept/Recv: %v", result.err)
	}
	if result.claims.Scope.TerminalID != "term-1" || !bytes.Equal(result.frame, wantFrame) {
		t.Fatalf("server result = %+v frame=%x", result.claims, result.frame)
	}
}

func TestClientRejectsDeviceHelloBoundToDifferentDTLSCertificate(t *testing.T) {
	identity, grant, now := handshakeFixture(t, Scope{AllowDaemon: true})
	clientConn, serverConn := memory.NewPair()
	serverDone := make(chan error, 1)
	go func() {
		_, err := (ServerHandshake{Identity: identity, Now: fixedNow(now)}).Accept(context.Background(), serverConn, fixtureDTLSFingerprint(0x11))
		serverDone <- err
	}()
	_, err := (ClientHandshake{Now: fixedNow(now)}).Authenticate(context.Background(), clientConn, ClientHandshakeRequest{
		ExpectedDeviceID:                 identity.DeviceID,
		ExpectedDeviceFingerprint:        identity.Fingerprint,
		CapabilityGrant:                  grant,
		DaemonDTLSCertificateFingerprint: fixtureDTLSFingerprint(0x22),
	})
	if HandshakeCodeOf(err) != remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH {
		t.Fatalf("Authenticate error = %v, want DEVICE_IDENTITY_MISMATCH", err)
	}
	_ = clientConn.Close()
	_ = serverConn.Close()
	<-serverDone
}

func TestServerRejectsRevokedCapabilityWithoutLeakingScope(t *testing.T) {
	identity, grant, now := handshakeFixture(t, Scope{TerminalID: "secret-terminal"})
	revocations := NewRevocations()
	revocations.Revoke("grant-1")
	clientConn, serverConn := memory.NewPair()
	defer clientConn.Close()
	defer serverConn.Close()
	serverDone := make(chan error, 1)
	go func() {
		_, err := (ServerHandshake{Identity: identity, Revocations: revocations, Now: fixedNow(now)}).Accept(context.Background(), serverConn, fixtureDTLSFingerprint(0x11))
		serverDone <- err
	}()
	_, err := (ClientHandshake{Now: fixedNow(now)}).Authenticate(context.Background(), clientConn, ClientHandshakeRequest{
		ExpectedDeviceID:                 identity.DeviceID,
		ExpectedDeviceFingerprint:        identity.Fingerprint,
		CapabilityGrant:                  grant,
		DaemonDTLSCertificateFingerprint: fixtureDTLSFingerprint(0x11),
	})
	if HandshakeCodeOf(err) != remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_REVOKED {
		t.Fatalf("Authenticate error = %v, want CAPABILITY_REVOKED", err)
	}
	if bytes.Contains([]byte(err.Error()), []byte("secret-terminal")) {
		t.Fatalf("rejection leaked terminal scope: %v", err)
	}
	if serverErr := <-serverDone; !errors.Is(serverErr, ErrGrantRevoked) {
		t.Fatalf("server error = %v, want ErrGrantRevoked", serverErr)
	}
}

func TestCapturedCapabilityOpenFailsAgainstNewChallenge(t *testing.T) {
	identity, grant, now := handshakeFixture(t, Scope{AllowDaemon: true})
	captured := completeManualHandshake(t, ServerHandshake{
		Identity: identity,
		Now:      fixedNow(now),
		Random:   bytes.NewReader(bytes.Repeat([]byte{0x31}, authSessionBytes+authNonceBytes)),
	}, grant, fixtureDTLSFingerprint(0x11))

	clientConn, serverConn := memory.NewPair()
	defer clientConn.Close()
	defer serverConn.Close()
	serverDone := make(chan error, 1)
	go func() {
		_, err := (ServerHandshake{
			Identity: identity,
			Now:      fixedNow(now),
			Random:   bytes.NewReader(bytes.Repeat([]byte{0x42}, authSessionBytes+authNonceBytes)),
		}).Accept(context.Background(), serverConn, fixtureDTLSFingerprint(0x11))
		serverDone <- err
	}()
	if _, err := receiveAuthEnvelope(context.Background(), clientConn); err != nil {
		t.Fatalf("receive new DeviceHello: %v", err)
	}
	if err := clientConn.Send(captured); err != nil {
		t.Fatalf("send captured CapabilityOpen: %v", err)
	}
	rejected, err := receiveAuthEnvelope(context.Background(), clientConn)
	if err != nil {
		t.Fatalf("receive replay rejection: %v", err)
	}
	if rejected.GetCapabilityRejected().GetCode() != remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_REPLAYED {
		t.Fatalf("replay result = %+v", rejected)
	}
	if serverErr := <-serverDone; HandshakeCodeOf(serverErr) != remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_REPLAYED {
		t.Fatalf("server replay error = %v", serverErr)
	}
}

func TestCrossPlatformCanonicalFixture(t *testing.T) {
	identity, grant, now := handshakeFixture(t, Scope{TerminalID: "term-1"})
	hello := &remoteauthpb.DeviceHello{
		DeviceId: identity.DeviceID, DevicePublicKey: identity.PublicKey, DeviceFingerprint: identity.Fingerprint,
		ServerNonce: bytes.Repeat([]byte{0x55}, authNonceBytes), DaemonDtlsCertificateFingerprint: fixtureDTLSFingerprint(0x11),
		IssuedAtUnixNano: now.UnixNano(),
	}
	signingBytes, err := DeviceHelloSigningBytes("fixture-auth-session-01", hello)
	if err != nil {
		t.Fatalf("DeviceHelloSigningBytes: %v", err)
	}
	proof, err := CalculateCapabilityProof(grant, "fixture-auth-session-01", hello.GetServerNonce(), bytes.Repeat([]byte{0x66}, authNonceBytes), fixtureDTLSFingerprint(0x11))
	if err != nil {
		t.Fatalf("CalculateCapabilityProof: %v", err)
	}
	const wantGrant = "termx-grant-v1.eyJ2ZXJzaW9uIjoxLCJncmFudF9pZCI6ImdyYW50LTEiLCJpc3N1ZXJfZGV2aWNlX2lkIjoiZGV2aWNlLTEiLCJpc3N1ZXJfZGV2aWNlX2ZpbmdlcnByaW50IjoiZWQyNTUxOS1zaGEyNTY6aWpnc0tqUGtMRTg1eVRvaERfRExxMXd6NmtnUEtMNWhTNTJpQ0s1ajlqUSIsInNjb3BlIjp7InRlcm1pbmFsX2lkIjoidGVybS0xIn0sImlzc3VlZF9hdCI6IjIwMjYtMDctMTFUMTI6MzM6NTYuNzg5WiIsIm5vdF9iZWZvcmUiOiIyMDI2LTA3LTExVDEyOjMzOjU2Ljc4OVoiLCJleHBpcmVzX2F0IjoiMjAyNi0wNy0xMVQxMzozNDo1Ni43ODlaIiwicmV2b2NhdGlvbl9pZCI6ImdyYW50LTEiLCJub25jZSI6ImZpeHR1cmUtZ3JhbnQtbm9uY2UifQ.dPhc2jTRwnxGIUhHMekVecPZxs_A2UsoGqEekWIFiqk.v3LHByQUV_Zv7tid79-kAyZnEO6Yt1lFQr6aChkTp-jKaQzl_BGFpcN153VTAAlKdKlcd0n6m-dl9VMKdfwTAw"
	const wantSigningHex = "0a117465726d782d72656d6f74652d6175746810011a17666978747572652d617574682d73657373696f6e2d303122086465766963652d312a2074f85cda34d1c27c4621484731e91579c3d9c6cfc0d94b281aa11e9162058aa9323a656432353531392d7368613235363a696a67734b6a506b4c45383579546f68445f444c7131777a366b67504b4c356853353269434b356a396a513a20555555555555555555555555555555555555555555555555555555555555555542677368612d3235363a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a31313a313148c09ebe97cd8bcfe018"
	const wantProofHex = "ead3095c3f5f371e7c9c154e57bce457a907c4f076d3e8203b018c93cb0f1d1a"
	if grant != wantGrant || hex.EncodeToString(signingBytes) != wantSigningHex || hex.EncodeToString(proof) != wantProofHex {
		t.Fatalf("canonical fixture mismatch\ngrant=%s\nsigning=%s\nproof=%s", grant, hex.EncodeToString(signingBytes), hex.EncodeToString(proof))
	}
}

func TestAuthEnvelopeRejectsTermxFrameAndUnknownFields(t *testing.T) {
	if _, err := UnmarshalAuthEnvelope([]byte{0, 0, 0, 0, 0, 0, 0}); HandshakeCodeOf(err) != remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL {
		t.Fatalf("termx frame error = %v, want PROTOCOL", err)
	}
	envelope := &remoteauthpb.AuthEnvelope{
		Protocol: AuthProtocol, Version: AuthVersion, AuthSessionId: "fixture-auth-session-01",
		Payload: &remoteauthpb.AuthEnvelope_CapabilityRejected{CapabilityRejected: &remoteauthpb.CapabilityRejected{
			Code: remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL,
		}},
	}
	frame, err := MarshalAuthEnvelope(envelope)
	if err != nil {
		t.Fatalf("MarshalAuthEnvelope: %v", err)
	}
	frame = append(frame, 0x80, 0x06, 0x01)
	if _, err := UnmarshalAuthEnvelope(frame); HandshakeCodeOf(err) != remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL {
		t.Fatalf("unknown field error = %v, want PROTOCOL", err)
	}
}

func completeManualHandshake(t *testing.T, server ServerHandshake, grant string, dtlsFingerprint string) []byte {
	t.Helper()
	clientConn, serverConn := memory.NewPair()
	defer clientConn.Close()
	defer serverConn.Close()
	serverDone := make(chan error, 1)
	go func() {
		_, err := server.Accept(context.Background(), serverConn, dtlsFingerprint)
		serverDone <- err
	}()
	helloEnvelope, err := receiveAuthEnvelope(context.Background(), clientConn)
	if err != nil {
		t.Fatalf("receive DeviceHello: %v", err)
	}
	clientNonce := bytes.Repeat([]byte{0x77}, authNonceBytes)
	proof, err := CalculateCapabilityProof(grant, helloEnvelope.GetAuthSessionId(), helloEnvelope.GetDeviceHello().GetServerNonce(), clientNonce, dtlsFingerprint)
	if err != nil {
		t.Fatalf("calculate proof: %v", err)
	}
	open := &remoteauthpb.AuthEnvelope{
		Protocol: AuthProtocol, Version: AuthVersion, AuthSessionId: helloEnvelope.GetAuthSessionId(),
		Payload: &remoteauthpb.AuthEnvelope_CapabilityOpen{CapabilityOpen: &remoteauthpb.CapabilityOpen{
			Grant: grant, ClientNonce: clientNonce, Proof: proof,
		}},
	}
	frame, err := MarshalAuthEnvelope(open)
	if err != nil {
		t.Fatalf("marshal CapabilityOpen: %v", err)
	}
	if err := clientConn.Send(frame); err != nil {
		t.Fatalf("send CapabilityOpen: %v", err)
	}
	accepted, err := receiveAuthEnvelope(context.Background(), clientConn)
	if err != nil || accepted.GetCapabilityAccepted() == nil {
		t.Fatalf("receive CapabilityAccepted: envelope=%+v err=%v", accepted, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server Accept: %v", err)
	}
	return append([]byte(nil), frame...)
}

func handshakeFixture(t *testing.T, scope Scope) (Identity, string, time.Time) {
	t.Helper()
	seed := bytes.Repeat([]byte{0x23}, ed25519.SeedSize)
	identity, err := NewIdentity("device-1", ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	now := time.Date(2026, 7, 11, 12, 34, 56, 789000000, time.UTC)
	grant, err := Issue(identity.PrivateKey, Claims{
		GrantID: "grant-1", IssuerDeviceID: identity.DeviceID, Scope: scope,
		IssuedAt: now.Add(-time.Minute), NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		RevocationID: "grant-1", Nonce: "fixture-grant-nonce",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return identity, grant, now
}

func fixtureDTLSFingerprint(octet byte) string {
	parts := make([]byte, 0, len("sha-256:")+32*3)
	parts = append(parts, "sha-256:"...)
	for index := 0; index < 32; index++ {
		if index > 0 {
			parts = append(parts, ':')
		}
		parts = append(parts, hex.EncodeToString([]byte{octet})...)
	}
	return string(parts)
}

func fixedNow(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

var _ transport.Transport = (*memory.Transport)(nil)
