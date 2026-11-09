package pairing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"
)

type Session struct {
	mu       sync.Mutex
	ID       string `json:"session_id"`
	Code     string `json:"code"`
	Public   string `json:"server_public_key"`
	Expires  int64  `json:"expires_at"`
	private  *ecdh.PrivateKey
	used     bool
	failures int
}
type Claim struct {
	Version      int    `json:"version"`
	SessionID    string `json:"session_id"`
	ClientPublic string `json:"client_public_key"`
	Sealed       string `json:"sealed_registration"`
	WantDirect   bool   `json:"want_direct_route"`
}

func New(now time.Time) (*Session, error) {
	private, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		return nil, e
	}
	id := make([]byte, 16)
	if _, e = rand.Read(id); e != nil {
		return nil, e
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	n, e := rand.Int(rand.Reader, big.NewInt(1000000))
	if e != nil {
		return nil, e
	}
	return &Session{ID: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), Code: fmt.Sprintf("%06d", n), Public: base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), Expires: now.Add(120 * time.Second).UnixMilli(), private: private}, nil
}
func derive(shared []byte, id, code string) []byte {
	extract := hmac.New(sha256.New, []byte(id))
	extract.Write(shared)
	expand := hmac.New(sha256.New, extract.Sum(nil))
	expand.Write([]byte("AirSIMPair/v1:" + code))
	expand.Write([]byte{1})
	return expand.Sum(nil)
}
func (s *Session) Open(c Claim, now time.Time) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used || s.failures >= 5 || now.UnixMilli() >= s.Expires || c.Version != 1 || c.SessionID != s.ID {
		return nil, errors.New("pairing session unavailable")
	}
	s.failures++
	p, e := base64.RawURLEncoding.DecodeString(c.ClientPublic)
	if e != nil {
		return nil, errors.New("invalid client key")
	}
	pub, e := ecdh.X25519().NewPublicKey(p)
	if e != nil {
		return nil, e
	}
	shared, e := s.private.ECDH(pub)
	if e != nil {
		return nil, e
	}
	block, e := aes.NewCipher(derive(shared, s.ID, s.Code))
	if e != nil {
		return nil, e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	sealed, e := base64.RawURLEncoding.DecodeString(c.Sealed)
	if e != nil || len(sealed) < 28 {
		return nil, errors.New("invalid sealed registration")
	}
	plaintext, e := gcm.Open(nil, sealed[:12], sealed[12:], []byte(s.ID))
	if e != nil {
		return nil, errors.New("pairing authentication failed")
	}
	s.used = true
	return plaintext, nil
}
func (s *Session) Info() map[string]any {
	return map[string]any{"product": "airsim-pair", "version": 1, "session_id": s.ID, "server_public_key": s.Public, "expires_at": s.Expires, "server_time": time.Now().UnixMilli()}
}
