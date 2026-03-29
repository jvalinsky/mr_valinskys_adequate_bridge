package secretstream

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream/boxstream"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/nacl/auth"
	"golang.org/x/crypto/nacl/box"
)

const NetworkString = "boxstream"

var (
	ErrInvalidKey      = errors.New("secretstream: invalid key")
	ErrHandshakeFailed = errors.New("secretstream: handshake failed")
)

type Addr struct {
	net.Addr
	PubKey []byte
}

func (a Addr) String() string {
	return base64.StdEncoding.EncodeToString(a.PubKey)
}

type AppKey [32]byte

func NewAppKey(s string) AppKey {
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

	out := make([]byte, 0, len(s.hello)-box.Overhead)
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

	s.hello = make([]byte, 0, len(data)-16)
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

	var out = make([]byte, 0, len(okay)+16)
	var nonce [24]byte
	return box.SealAfterPrecomputation(out, okay[:], &nonce, &s.secret3)
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
	copy(nonce[:], s.remoteAppMac[:])
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
	copy(nonce[:], s.localAppMac[:])
	return deKey, nonce
}

func ed25519PublicToCurve25519(edPub ed25519.PublicKey) [32]byte {
	var curvePub [32]byte
	curve25519.ScalarBaseMult(&curvePub, (*[32]byte)(edPub))
	return curvePub
}

func ed25519PrivateToCurve25519(edPriv ed25519.PrivateKey) [32]byte {
	var curvePriv [32]byte
	var base [32]byte
	curve25519.ScalarMult(&curvePriv, &base, (*[32]byte)(edPriv))
	return curvePriv
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
	challenge := c.state.createChallenge()
	if _, err := c.conn.Write(challenge); err != nil {
		return err
	}

	resp := make([]byte, 64)
	if _, err := io.ReadFull(c.conn, resp); err != nil {
		return err
	}
	if !c.state.verifyChallenge(resp) {
		return ErrHandshakeFailed
	}

	clientAuth := c.state.createClientAuth()
	if _, err := c.conn.Write(clientAuth); err != nil {
		return err
	}

	serverAccept := make([]byte, 64+16)
	if _, err := io.ReadFull(c.conn, serverAccept); err != nil {
		return err
	}
	if !c.state.verifyServerAccept(serverAccept) {
		return ErrHandshakeFailed
	}

	c.state.cleanSecrets()

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

func (c *Client) Close() error {
	c.boxer.WriteGoodbye()
	return c.conn.Close()
}

func (c *Client) RemoteAddr() net.Addr {
	return Addr{
		Addr:   c.conn.RemoteAddr(),
		PubKey: c.state.Remote(),
	}
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
	challenge := make([]byte, 64)
	if _, err := io.ReadFull(s.conn, challenge); err != nil {
		return err
	}
	if !s.state.verifyChallenge(challenge) {
		return ErrHandshakeFailed
	}

	challengeResp := s.state.createChallenge()
	if _, err := s.conn.Write(challengeResp); err != nil {
		return err
	}

	clientAuth := make([]byte, 80)
	if _, err := io.ReadFull(s.conn, clientAuth); err != nil {
		return err
	}
	if !s.state.verifyClientAuth(clientAuth) {
		return ErrHandshakeFailed
	}

	serverAccept := s.state.createServerAccept()
	if _, err := s.conn.Write(serverAccept); err != nil {
		return err
	}

	s.state.cleanSecrets()

	encKey, encNonce := s.state.GetBoxstreamEncKeys()
	decKey, decNonce := s.state.GetBoxstreamDecKeys()

	s.boxer = boxstream.NewBoxer(s.conn, &encNonce, &encKey)
	s.unboxer = boxstream.NewUnboxer(s.conn, &decNonce, &decKey)

	return nil
}

func (s *Server) WriteMessage(msg []byte) error {
	return s.boxer.WriteMessage(msg)
}

func (s *Server) ReadMessage() ([]byte, error) {
	return s.unboxer.ReadMessage()
}

func (s *Server) Close() error {
	s.boxer.WriteGoodbye()
	return s.conn.Close()
}

func (s *Server) RemoteAddr() net.Addr {
	return Addr{
		Addr:   s.conn.RemoteAddr(),
		PubKey: s.state.Remote(),
	}
}
