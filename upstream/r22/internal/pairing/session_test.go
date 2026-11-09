package pairing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
)

// Generated independently with Node crypto.hkdfSync/createCipheriv and the
// RFC7748 Alice-private/Bob-public shared secret. Matches the iOS/Java wire format.
func TestIndependentPairingVector(t *testing.T) {
	privateRaw, _ := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	private, _ := ecdh.X25519().NewPrivateKey(privateRaw)
	bobRaw, _ := hex.DecodeString("de9edb7d7b7dc1b4d35b61c2ece435373f8343c85b78674dadfc7e146f882b4f")
	s := &Session{ID: "test-session", Code: "123456", private: private, Expires: time.Now().Add(time.Minute).UnixMilli()}
	c := Claim{Version: 1, SessionID: s.ID, ClientPublic: base64.RawURLEncoding.EncodeToString(bobRaw), Sealed: "BwcHBwcHBwcHBwcH1uQF3fcFmhLQfEWgE3k57IfomxvkV_YGYS7QdaBc01XAWkw3iDar"}
	plaintext, e := s.Open(c, time.Now())
	if e != nil || string(plaintext) != `{"device_id":"fixture"}` {
		t.Fatalf("wire compatibility: %s %v", plaintext, e)
	}
}

func sealed(t *testing.T, s *Session) Claim {
	t.Helper()
	client, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := base64.RawURLEncoding.DecodeString(s.Public)
	pub, _ := ecdh.X25519().NewPublicKey(p)
	shared, _ := client.ECDH(pub)
	block, _ := aes.NewCipher(derive(shared, s.ID, s.Code))
	g, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	rand.Read(nonce)
	sealed := append(nonce, g.Seal(nil, nonce, []byte(`{"device_id":"test"}`), []byte(s.ID))...)
	return Claim{Version: 1, SessionID: s.ID, ClientPublic: base64.RawURLEncoding.EncodeToString(client.PublicKey().Bytes()), Sealed: base64.RawURLEncoding.EncodeToString(sealed)}
}
func TestClaimAndReplay(t *testing.T) {
	s, _ := New(time.Now())
	c := sealed(t, s)
	p, e := s.Open(c, time.Now())
	if e != nil || string(p) != `{"device_id":"test"}` {
		t.Fatal(e)
	}
	if _, e = s.Open(c, time.Now()); e == nil {
		t.Fatal("replay accepted")
	}
}
func TestExpired(t *testing.T) {
	s, _ := New(time.Now().Add(-3 * time.Minute))
	if _, e := s.Open(sealed(t, s), time.Now()); e == nil {
		t.Fatal("expired accepted")
	}
}
func TestTamperedAAD(t *testing.T) {
	s, _ := New(time.Now())
	c := sealed(t, s)
	s.Code = "wrong"
	if _, e := s.Open(c, time.Now()); e == nil {
		t.Fatal("wrong code accepted")
	}
}
