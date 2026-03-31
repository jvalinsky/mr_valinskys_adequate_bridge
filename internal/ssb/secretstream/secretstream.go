package secretstream

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream/boxstream"

	"crypto/sha512"
	"filippo.io/edwards25519"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/nacl/auth"
	"golang.org/x/crypto/nacl/box"
	"time"
)

const NetworkString = "boxstream"

// Standard SSB public network identifier (Base64: 1KHLiKZvAvjbY1ziZEHMXawbCEIM6qwjCDm3VYRan/s=)
var DefaultAppKey = AppKey{
	0xd4, 0xa1, 0xcb, 0x88, 0xa6, 0x6f, 0x02, 0xf8,
	0xdb, 0x63, 0x5c, 0xe2, 0x64, 0x41, 0xcc, 0x5d,
	0xac, 0x1b, 0x08, 0x42, 0x0c, 0xea, 0xac, 0x23,
	0x08, 0x39, 0xb7, 0x55, 0x84, 0x5a, 0x9f, 0xfb,
}

var (
	ErrInvalidKey      = errors.New("secretstream: invalid key")
	ErrHandshakeFailed = errors.New("secretstream: handshake failed")
	ErrNeedInput       = errors.New("secretstream: need more input")
)

type Handshaker interface {
	Next(input []byte) (output []byte, err error)
	IsDone() bool
	State() *State
}

type shsState int

const (
	shsInitial shsState = iota
	shsWaitingChallenge
	shsWaitingClientAuth
	shsWaitingServerAccept
	shsDone
)

type HandshakeMachine struct {
	state    *State
	role     shsState
	isClient bool
	buffer   []byte
}

func NewClientHandshake(appKey AppKey, local ed25519.PrivateKey, remote ed25519.PublicKey) (*HandshakeMachine, error) {
	state, err := NewClientState(appKey, local, remote)
	if err != nil {
		return nil, err
	}
	return &HandshakeMachine{
		state:    state,
		role:     shsInitial,
		isClient: true,
	}, nil
}

func NewServerHandshake(appKey AppKey, local ed25519.PrivateKey) (*HandshakeMachine, error) {
	state, err := NewServerState(appKey, local)
	if err != nil {
		return nil, err
	}
	return &HandshakeMachine{
		state:    state,
		role:     shsInitial,
		isClient: false,
	}, nil
}

func (m *HandshakeMachine) IsDone() bool {
	return m.role == shsDone
}

func (m *HandshakeMachine) State() *State {
	return m.state
}

func (m *HandshakeMachine) ExpectedBytes() int {
	if m.IsDone() {
		return 0
	}
	if m.isClient {
		switch m.role {
		case shsWaitingChallenge:
			return 64
		case shsWaitingServerAccept:
			return 80
		default:
			return 0
		}
	} else {
		switch m.role {
		case shsInitial:
			return 64
		case shsWaitingClientAuth:
			return 112
		default:
			return 0
		}
	}
}

func (m *HandshakeMachine) Next(input []byte) ([]byte, error) {
	if m.role == shsDone {
		return nil, nil
	}

	if len(input) > 0 {
		m.buffer = append(m.buffer, input...)
	}

	if m.isClient {
		return m.nextClient()
	}
	return m.nextServer()
}

func (m *HandshakeMachine) nextClient() ([]byte, error) {
	switch m.role {
	case shsInitial:
		challenge := m.state.createChallenge()
		m.role = shsWaitingChallenge
		return challenge, nil

	case shsWaitingChallenge:
		if len(m.buffer) < 64 {
			return nil, ErrNeedInput
		}
		challenge := m.buffer[:64]
		m.buffer = m.buffer[64:]

		if !m.state.verifyChallenge(challenge) {
			return nil, ErrHandshakeFailed
		}

		clientAuth := m.state.createClientAuth()
		m.role = shsWaitingServerAccept
		return clientAuth, nil

	case shsWaitingServerAccept:
		if len(m.buffer) < 80 {
			return nil, ErrNeedInput
		}
		serverAccept := m.buffer[:80]
		m.buffer = m.buffer[80:]

		if !m.state.verifyServerAccept(serverAccept) {
			return nil, ErrHandshakeFailed
		}

		m.state.cleanSecrets()
		m.role = shsDone
		return nil, nil

	default:
		return nil, nil
	}
}

func (m *HandshakeMachine) nextServer() ([]byte, error) {
	switch m.role {
	case shsInitial:
		if len(m.buffer) < 64 {
			return nil, ErrNeedInput
		}
		challenge := m.buffer[:64]
		m.buffer = m.buffer[64:]

		if !m.state.verifyChallenge(challenge) {
			return nil, ErrHandshakeFailed
		}

		challengeResp := m.state.createChallenge()
		m.role = shsWaitingClientAuth
		return challengeResp, nil

	case shsWaitingClientAuth:
		if len(m.buffer) < 112 {
			return nil, ErrNeedInput
		}
		clientAuth := m.buffer[:112]
		m.buffer = m.buffer[112:]

		if !m.state.verifyClientAuth(clientAuth) {
			return nil, ErrHandshakeFailed
		}

		serverAccept := m.state.createServerAccept()
		m.role = shsWaitingServerAccept
		m.state.cleanSecrets()
		m.role = shsDone

		return serverAccept, nil

	default:
		return nil, nil
	}
}

type Addr struct {
	net.Addr
	PubKey []byte
}

func (a Addr) String() string {
	return base64.StdEncoding.EncodeToString(a.PubKey)
}

type AppKey [32]byte

func NewAppKey(s string) AppKey {
	if s == "" || s == "boxstream" {
		return DefaultAppKey
	}
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
			var k AppKey
			copy(k[:], b)
			return k
		}
	}
	h := sha256.Sum256([]byte(s))
	return h
}

type State struct {
	appKey [32]byte

	secHash      []byte
	localAppMac  [32]byte
	remoteAppMac [32]byte

	localExchange  [32]byte
	localExchangeS [32]byte
	remoteExchange [32]byte
	local          ed25519.PrivateKey
	remotePublic   ed25519.PublicKey

	secret, secret2, secret3 [32]byte

	hello []byte

	aBob, bAlice [32]byte
}

func NewClientState(appKey AppKey, local ed25519.PrivateKey, remotePublic ed25519.PublicKey) (*State, error) {
	s, err := newState(appKey, local)
	if err != nil {
		return nil, err
	}
	s.remotePublic = remotePublic
	return s, nil
}

func NewServerState(appKey AppKey, local ed25519.PrivateKey) (*State, error) {
	return newState(appKey, local)
}

func newState(appKey AppKey, local ed25519.PrivateKey) (*State, error) {
	pubKey, secKey, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	s := &State{
		remotePublic: make([]byte, ed25519.PublicKeySize),
	}
	copy(s.appKey[:], appKey[:])
	copy(s.localExchange[:], pubKey[:])
	copy(s.localExchangeS[:], secKey[:])
	s.local = local

	return s, nil
}

func (s *State) createChallenge() []byte {
	mac := auth.Sum(s.localExchange[:], &s.appKey)
	copy(s.localAppMac[:], mac[:])
	return append(s.localAppMac[:], s.localExchange[:]...)
}

func (s *State) verifyChallenge(ch []byte) bool {
	mac := ch[:32]
	remoteEphPubKey := ch[32:]

	ok := auth.Verify(mac, remoteEphPubKey, &s.appKey)

	copy(s.remoteExchange[:], remoteEphPubKey)
	copy(s.remoteAppMac[:], mac)

	var sec [32]byte
	curve25519.ScalarMult(&sec, &s.localExchangeS, &s.remoteExchange)
	copy(s.secret[:], sec[:])

	secHasher := sha256.New()
	secHasher.Write(s.secret[:])
	s.secHash = secHasher.Sum(nil)

	return ok
}

func (s *State) createClientAuth() []byte {
	curveRemotePub := ed25519PublicToCurve25519(s.remotePublic)

	var aBob [32]byte
	curve25519.ScalarMult(&aBob, &s.localExchangeS, &curveRemotePub)
	copy(s.aBob[:], aBob[:])

	secHasher := sha256.New()
	secHasher.Write(s.appKey[:])
	secHasher.Write(s.secret[:])
	secHasher.Write(s.aBob[:])
	copy(s.secret2[:], secHasher.Sum(nil))

	sigMsg := make([]byte, 0, 32+32+32)
	sigMsg = append(sigMsg, s.appKey[:]...)
	sigMsg = append(sigMsg, s.remotePublic[:32]...)
	sigMsg = append(sigMsg, s.secHash[:]...)

	sig := ed25519.Sign(s.local, sigMsg)

	pub := s.local.Public().(ed25519.PublicKey)
	s.hello = make([]byte, 0, len(sig)+len(pub))
	s.hello = append(s.hello, sig[:]...)
	s.hello = append(s.hello, pub[:]...)

	out := make([]byte, 0, len(s.hello)+box.Overhead)
	var n [24]byte
	out = box.SealAfterPrecomputation(out, s.hello, &n, &s.secret2)
	return out
}

func (s *State) verifyClientAuth(data []byte) bool {
	curveLocalSec := ed25519PrivateToCurve25519(s.local)

	var aBob [32]byte
	curve25519.ScalarMult(&aBob, &curveLocalSec, &s.remoteExchange)
	copy(s.aBob[:], aBob[:])

	secHasher := sha256.New()
	secHasher.Write(s.appKey[:])
	secHasher.Write(s.secret[:])
	secHasher.Write(s.aBob[:])
	copy(s.secret2[:], secHasher.Sum(nil))

	s.hello = make([]byte, 0, len(data)-box.Overhead)
	var nonce [24]byte
	var openOk bool
	s.hello, openOk = box.OpenAfterPrecomputation(s.hello, data, &nonce, &s.secret2)
	if !openOk {
		return false
	}

	sig := s.hello[:64]
	public := s.hello[64:]

	copy(s.remotePublic[:], public[:32])

	sigMsg := make([]byte, 0, 32+32+32)
	pubLocal := s.local.Public().(ed25519.PublicKey)
	sigMsg = append(sigMsg, s.appKey[:]...)
	sigMsg = append(sigMsg, pubLocal[:]...)
	sigMsg = append(sigMsg, s.secHash[:]...)

	return ed25519.Verify(public[:32], sigMsg, sig)
}

func (s *State) createServerAccept() []byte {
	curveRemotePub := ed25519PublicToCurve25519(s.remotePublic)

	var bAlice [32]byte
	curve25519.ScalarMult(&bAlice, &s.localExchangeS, &curveRemotePub)
	copy(s.bAlice[:], bAlice[:])

	secHasher := sha256.New()
	secHasher.Write(s.appKey[:])
	secHasher.Write(s.secret[:])
	secHasher.Write(s.aBob[:])
	secHasher.Write(s.bAlice[:])
	copy(s.secret3[:], secHasher.Sum(nil))

	sigMsg := make([]byte, 0, 32+len(s.hello)+32)
	sigMsg = append(sigMsg, s.appKey[:]...)
	sigMsg = append(sigMsg, s.hello[:]...)
	sigMsg = append(sigMsg, s.secHash[:]...)

	okay := ed25519.Sign(s.local, sigMsg)

	var out = make([]byte, 0, len(okay)+box.Overhead)
	var nonce [24]byte
	return box.SealAfterPrecomputation(out, okay, &nonce, &s.secret3)
}

func (s *State) verifyServerAccept(boxedOkay []byte) bool {
	curveLocalSec := ed25519PrivateToCurve25519(s.local)

	var bAlice [32]byte
	curve25519.ScalarMult(&bAlice, &curveLocalSec, &s.remoteExchange)
	copy(s.bAlice[:], bAlice[:])

	secHasher := sha256.New()
	secHasher.Write(s.appKey[:])
	secHasher.Write(s.secret[:])
	secHasher.Write(s.aBob[:])
	secHasher.Write(s.bAlice[:])
	copy(s.secret3[:], secHasher.Sum(nil))

	var nonce [24]byte
	sig, openOk := box.OpenAfterPrecomputation(nil, boxedOkay, &nonce, &s.secret3)
	if !openOk {
		return false
	}

	sigMsg := make([]byte, 0, 32+len(s.hello)+32)
	sigMsg = append(sigMsg, s.appKey[:]...)
	sigMsg = append(sigMsg, s.hello[:]...)
	sigMsg = append(sigMsg, s.secHash[:]...)

	return ed25519.Verify(s.remotePublic[:32], sigMsg, sig)
}

func (s *State) cleanSecrets() {
	var zeros [64]byte
	copy(s.secHash, zeros[:])
	copy(s.secret[:], zeros[:])
	copy(s.aBob[:], zeros[:])
	copy(s.bAlice[:], zeros[:])

	h := sha256.New()
	h.Write(s.secret3[:])
	copy(s.secret[:], h.Sum(nil))
	copy(s.secret2[:], zeros[:])
	copy(s.secret3[:], zeros[:])
	copy(s.localExchangeS[:], zeros[:])
}

func (s *State) Remote() []byte {
	return s.remotePublic[:]
}

func (s *State) GetBoxstreamEncKeys() ([32]byte, [24]byte) {
	var enKey [32]byte
	h := sha256.New()
	h.Write(s.secret[:])
	h.Write(s.remotePublic[:32])
	copy(enKey[:], h.Sum(nil))

	var nonce [24]byte
	copy(nonce[:], s.localAppMac[:])
	return enKey, nonce
}

func (s *State) GetBoxstreamDecKeys() ([32]byte, [24]byte) {
	var deKey [32]byte
	h := sha256.New()
	h.Write(s.secret[:])
	pub := s.local.Public().(ed25519.PublicKey)
	h.Write(pub[:])
	copy(deKey[:], h.Sum(nil))

	var nonce [24]byte
	copy(nonce[:], s.remoteAppMac[:])
	return deKey, nonce
}

func ed25519PublicToCurve25519(edPub ed25519.PublicKey) [32]byte {
	pt, err := new(edwards25519.Point).SetBytes(edPub)
	if err != nil {
		return [32]byte{}
	}
	var out [32]byte
	copy(out[:], pt.BytesMontgomery())
	return out
}

func ed25519PrivateToCurve25519(edPriv ed25519.PrivateKey) [32]byte {
	h := sha512.Sum512(edPriv[:32])
	s := h[:32]
	s[0] &= 248
	s[31] &= 127
	s[31] |= 64
	var out [32]byte
	copy(out[:], s)
	return out
}

type Client struct {
	conn    net.Conn
	state   *State
	boxer   *boxstream.Boxer
	unboxer *boxstream.Unboxer
}

func NewClient(conn net.Conn, appKey AppKey, local ed25519.PrivateKey, remote ed25519.PublicKey) (*Client, error) {
	state, err := NewClientState(appKey, local, remote)
	if err != nil {
		return nil, err
	}

	c := &Client{conn: conn, state: state}
	return c, nil
}

func (c *Client) Handshake() error {
	m := &HandshakeMachine{
		state:    c.state,
		role:     shsInitial,
		isClient: true,
	}

	// Client Handshake Loop
	output, err := m.Next(nil)
	if err != nil {
		return err
	}
	if _, err := c.conn.Write(output); err != nil {
		return err
	}

	for !m.IsDone() {
		expected := m.ExpectedBytes()
		if expected == 0 {
			return ErrHandshakeFailed
		}

		input := make([]byte, expected)
		if _, err := io.ReadFull(c.conn, input); err != nil {
			return err
		}

		output, err = m.Next(input)
		if err != nil {
			return err
		}
		if len(output) > 0 {
			if _, err := c.conn.Write(output); err != nil {
				return err
			}
		}
	}

	encKey, encNonce := c.state.GetBoxstreamEncKeys()
	decKey, decNonce := c.state.GetBoxstreamDecKeys()

	c.boxer = boxstream.NewBoxer(c.conn, &encNonce, &encKey)
	c.unboxer = boxstream.NewUnboxer(c.conn, &decNonce, &decKey)

	return nil
}

func (c *Client) WriteMessage(msg []byte) error {
	return c.boxer.WriteMessage(msg)
}

func (c *Client) ReadMessage() ([]byte, error) {
	return c.unboxer.ReadMessage()
}

type Server struct {
	conn    net.Conn
	state   *State
	boxer   *boxstream.Boxer
	unboxer *boxstream.Unboxer
}

func NewServer(conn net.Conn, appKey AppKey, local ed25519.PrivateKey) (*Server, error) {
	state, err := NewServerState(appKey, local)
	if err != nil {
		return nil, err
	}

	s := &Server{conn: conn, state: state}
	return s, nil
}

func (s *Server) Handshake() error {
	m := &HandshakeMachine{
		state:    s.state,
		role:     shsInitial,
		isClient: false,
	}

	for !m.IsDone() {
		expected := m.ExpectedBytes()
		if expected == 0 {
			return ErrHandshakeFailed
		}

		input := make([]byte, expected)
		if _, err := io.ReadFull(s.conn, input); err != nil {
			return err
		}

		output, err := m.Next(input)
		if err != nil {
			return err
		}
		if len(output) > 0 {
			if _, err := s.conn.Write(output); err != nil {
				return err
			}
		}
	}

	encKey, encNonce := s.state.GetBoxstreamEncKeys()
	decKey, decNonce := s.state.GetBoxstreamDecKeys()

	s.boxer = boxstream.NewBoxer(s.conn, &encNonce, &encKey)
	s.unboxer = boxstream.NewUnboxer(s.conn, &decNonce, &decKey)

	return nil
}

func (s *Server) Read(b []byte) (int, error) {
	return s.unboxer.Read(b)
}

func (s *Server) Write(b []byte) (int, error) {
	return s.boxer.Write(b)
}

func (s *Server) Close() error {
	s.boxer.WriteGoodbye()
	return s.conn.Close()
}

func (s *Server) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

func (s *Server) RemoteAddr() net.Addr {
	return Addr{
		Addr:   s.conn.RemoteAddr(),
		PubKey: s.state.Remote(),
	}
}

func (s *Server) SetDeadline(t time.Time) error      { return s.conn.SetDeadline(t) }
func (s *Server) SetReadDeadline(t time.Time) error  { return s.conn.SetReadDeadline(t) }
func (s *Server) SetWriteDeadline(t time.Time) error { return s.conn.SetWriteDeadline(t) }

func (s *Server) RemotePubKey() []byte {
	return s.state.Remote()
}

func (c *Client) Read(b []byte) (int, error) {
	return c.unboxer.Read(b)
}

func (c *Client) Write(b []byte) (int, error) {
	return c.boxer.Write(b)
}

func (c *Client) Close() error {
	c.boxer.WriteGoodbye()
	return c.conn.Close()
}

func (c *Client) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *Client) RemoteAddr() net.Addr {
	return Addr{
		Addr:   c.conn.RemoteAddr(),
		PubKey: c.state.Remote(),
	}
}

func (c *Client) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *Client) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *Client) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
