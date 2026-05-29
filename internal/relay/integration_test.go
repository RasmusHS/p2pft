package relay_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RasmusHS/p2pft/internal/relay"
	"github.com/RasmusHS/p2pft/internal/signaling"
)

// TestSignalingHandshake exercises the full sender ↔ relay ↔ receiver flow:
//
//	sender:                      receiver:
//	  → SenderHello
//	  ← SessionCreated
//	  → PeerAddrs
//	                              → ReceiverHello{code}
//	                              ← SessionFound (sender's metadata + addrs)
//	                              → PeerAddrs
//	  ← ReceiverJoined (receiver's addrs)
func TestSignalingHandshake(t *testing.T) {
	srv := relay.NewServer()
	defer srv.Shutdown()

	ts := httptest.NewServer(srv)
	defer ts.Close()
	wsURL := httpToWS(t, ts.URL) + "/ws"

	// Fixtures
	wantHello := signaling.SenderHello{
		Filename: "report.pdf",
		Size:     1024,
		Sha256:   "abc123def456",
	}
	wantSenderLocal := "192.168.1.42:54321"
	wantSenderFP := "feedfacecafebeef"

	wantReceiverLocal := "192.168.1.99:65432"
	wantReceiverFP := "deadbeef01234567"

	codeCh := make(chan string, 1)
	sendErr := make(chan error, 1)
	recvErr := make(chan error, 1)

	// Sender role
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		client, err := signaling.Dial(ctx, wsURL)
		if err != nil {
			sendErr <- err
			return
		}
		defer client.Close()

		// 1. Send SenderHello
		if err := client.Send(ctx, signaling.TypeSenderHello, wantHello); err != nil {
			sendErr <- err
			return
		}

		// 2. Expect SessionCreated
		env, err := client.Read(ctx)
		if err != nil {
			sendErr <- err
			return
		}
		if env.Type != signaling.TypeSessionCreated {
			sendErr <- &assertErr{want: signaling.TypeSessionCreated, got: env.Type}
			return
		}
		var created signaling.SessionCreated
		if err := signaling.DecodePayload(env, &created); err != nil {
			sendErr <- err
			return
		}
		if created.Code == "" {
			sendErr <- errors.New("expected non-empty code")
			return
		}
		if created.ExpiresIn <= 0 {
			sendErr <- errors.New("expected positive ExpiresIn")
			return
		}
		codeCh <- created.Code

		// 3. Send our PeerAddrs
		if err := client.Send(ctx, signaling.TypePeerAddrs, signaling.PeerAddrs{
			Local:           wantSenderLocal,
			CertFingerprint: wantSenderFP,
		}); err != nil {
			sendErr <- err
			return
		}

		// 4. Park, waiting for ReceiverJoined
		env, err = client.Read(ctx)
		if err != nil {
			sendErr <- err
			return
		}
		if env.Type != signaling.TypeReceiverJoined {
			sendErr <- &assertErr{want: signaling.TypeReceiverJoined, got: env.Type}
			return
		}
		var joined signaling.ReceiverJoined
		if err := signaling.DecodePayload(env, &joined); err != nil {
			sendErr <- err
			return
		}
		if joined.Peer.Local != wantReceiverLocal {
			sendErr <- &fieldErr{field: "joined.Peer.Local", want: wantReceiverLocal, got: joined.Peer.Local}
			return
		}
		if joined.Peer.CertFingerprint != wantReceiverFP {
			sendErr <- &fieldErr{field: "joined.Peer.CertFingerprint", want: wantReceiverFP, got: joined.Peer.CertFingerprint}
			return
		}
		// Relay should have observed and filled in the receiver's Public.
		// We can't predict the exact port but it should be non-empty.
		if joined.Peer.Public == "" {
			sendErr <- errors.New("relay did not fill in joined.Peer.Public")
			return
		}
		sendErr <- nil
	}()

	// Receiver role
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Wait for the sender to register and report its code.
		var code string
		select {
		case code = <-codeCh:
		case <-ctx.Done():
			recvErr <- errors.New("timed out waiting for sender to publish code")
			return
		}

		client, err := signaling.Dial(ctx, wsURL)
		if err != nil {
			recvErr <- err
			return
		}
		defer client.Close()

		// 1. Send ReceiverHello{code}
		if err := client.Send(ctx, signaling.TypeReceiverHello, signaling.ReceiverHello{
			Code: code,
		}); err != nil {
			recvErr <- err
			return
		}

		// 2. Expect SessionFound
		env, err := client.Read(ctx)
		if err != nil {
			recvErr <- err
			return
		}
		if env.Type != signaling.TypeSessionFound {
			recvErr <- &assertErr{want: signaling.TypeSessionFound, got: env.Type}
			return
		}
		var found signaling.SessionFound
		if err := signaling.DecodePayload(env, &found); err != nil {
			recvErr <- err
			return
		}
		if found.Filename != wantHello.Filename {
			recvErr <- &fieldErr{field: "found.Filename", want: wantHello.Filename, got: found.Filename}
			return
		}
		if found.Size != wantHello.Size {
			recvErr <- errors.New("found.Size mismatch")
			return
		}
		if found.Sha256 != wantHello.Sha256 {
			recvErr <- &fieldErr{field: "found.Sha256", want: wantHello.Sha256, got: found.Sha256}
			return
		}
		if found.Peer.Local != wantSenderLocal {
			recvErr <- &fieldErr{field: "found.Peer.Local", want: wantSenderLocal, got: found.Peer.Local}
			return
		}
		if found.Peer.CertFingerprint != wantSenderFP {
			recvErr <- &fieldErr{field: "found.Peer.CertFingerprint", want: wantSenderFP, got: found.Peer.CertFingerprint}
			return
		}
		if found.Peer.Public == "" {
			recvErr <- errors.New("relay did not fill in found.Peer.Public")
			return
		}

		// 3. Send our PeerAddrs
		if err := client.Send(ctx, signaling.TypePeerAddrs, signaling.PeerAddrs{
			Local:           wantReceiverLocal,
			CertFingerprint: wantReceiverFP,
		}); err != nil {
			recvErr <- err
			return
		}
		recvErr <- nil
	}()

	// Wait for both sides
	for i, ch := range []chan error{sendErr, recvErr} {
		select {
		case err := <-ch:
			if err != nil {
				if i == 0 {
					t.Errorf("sender: %v", err)
				} else {
					t.Errorf("receiver: %v", err)
				}
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for goroutine %d", i)
		}
	}
}

// TestUnknownCode verifies the receiver gets SessionError for a bad code.
func TestUnknownCode(t *testing.T) {
	srv := relay.NewServer()
	defer srv.Shutdown()

	ts := httptest.NewServer(srv)
	defer ts.Close()
	wsURL := httpToWS(t, ts.URL) + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := signaling.Dial(ctx, wsURL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if err := client.Send(ctx, signaling.TypeReceiverHello, signaling.ReceiverHello{
		Code: "nonexistent-bogus-code",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	env, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if env.Type != signaling.TypeSessionError {
		t.Fatalf("expected %q, got %q", signaling.TypeSessionError, env.Type)
	}
	var sessErr signaling.SessionError
	if err := signaling.DecodePayload(env, &sessErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sessErr.Reason == "" {
		t.Errorf("expected non-empty reason")
	}
}

// TestBadFirstMessage verifies an unknown envelope type gets SessionError.
func TestBadFirstMessage(t *testing.T) {
	srv := relay.NewServer()
	defer srv.Shutdown()

	ts := httptest.NewServer(srv)
	defer ts.Close()
	wsURL := httpToWS(t, ts.URL) + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := signaling.Dial(ctx, wsURL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// PeerAddrs is a real type but not valid as a first message.
	if err := client.Send(ctx, signaling.TypePeerAddrs, signaling.PeerAddrs{
		Local: "1.2.3.4:5",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	env, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if env.Type != signaling.TypeSessionError {
		t.Fatalf("expected %q, got %q", signaling.TypeSessionError, env.Type)
	}
}

// --- helpers ---

func httpToWS(t *testing.T, httpURL string) string {
	t.Helper()
	switch {
	case strings.HasPrefix(httpURL, "https://"):
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	case strings.HasPrefix(httpURL, "http://"):
		return "ws://" + strings.TrimPrefix(httpURL, "http://")
	default:
		t.Fatalf("unexpected URL scheme: %s", httpURL)
		return ""
	}
}

type assertErr struct {
	want, got string
}

func (a *assertErr) Error() string {
	return "expected message type " + a.want + ", got " + a.got
}

type fieldErr struct {
	field, want, got string
}

func (f *fieldErr) Error() string {
	return f.field + ": want " + f.want + ", got " + f.got
}
