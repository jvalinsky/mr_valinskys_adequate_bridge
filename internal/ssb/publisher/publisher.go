package publisher

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/crypto"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type Publisher struct {
	feed    refs.FeedRef
	keyPair *keys.KeyPair
	log     feedlog.Log
	hmacKey []byte
}

type Options struct {
	HMACKey []byte
}

type Option func(*Options)

func WithHMAC(key []byte) Option {
	return func(o *Options) {
		o.HMACKey = key
	}
}

func New(keyPair *keys.KeyPair, receiveLog feedlog.Log, users feedlog.MultiLog, opts ...Option) (*Publisher, error) {
	feed := keyPair.FeedRef()
	feedStr := feed.String()

	log, err := users.Get(feedStr)
	if err == feedlog.ErrNotFound {
		log, err = users.Create(feedStr)
	}
	if err != nil {
		return nil, fmt.Errorf("publisher: failed to get/create log: %w", err)
	}

	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	return &Publisher{
		feed:    feed,
		keyPair: keyPair,
		log:     log,
		hmacKey: o.HMACKey,
	}, nil
}

func (p *Publisher) Publish(content []byte) (refs.MessageRef, error) {
	seq, err := p.log.Seq()
	if err != nil {
		return refs.MessageRef{}, fmt.Errorf("publisher: failed to get seq: %w", err)
	}

	var previous *refs.MessageRef
	nextSeq := int64(1)
	if seq >= 0 {
		msg, err := p.log.Get(seq)
		if err == nil {
			prevRef, err := refs.ParseMessageRef(msg.Key)
			if err == nil {
				previous = prevRef
			}
			nextSeq = seq + 1
		}
	}

	var contentObj interface{}
	if err := json.Unmarshal(content, &contentObj); err != nil {
		contentObj = string(content)
	}

	msg := &legacy.Message{
		Previous:  previous,
		Author:    p.feed,
		Sequence:  nextSeq,
		Timestamp: time.Now().UnixMilli(),
		Hash:      "sha256",
		Content:   contentObj,
	}

	msgRef, sig, err := msg.Sign(p.keyPair, p.hmacKey)
	if err != nil {
		return refs.MessageRef{}, fmt.Errorf("publisher: failed to sign: %w", err)
	}

	metadata := &feedlog.Metadata{
		Author:    p.feed.String(),
		Sequence:  msg.Sequence,
		Previous:  "",
		Timestamp: int64(msg.Timestamp),
		Sig:       sig,
		Hash:      msgRef.String(),
	}

	if previous != nil {
		metadata.Previous = previous.String()
	}

	_, err = p.log.Append(content, metadata)
	if err != nil {
		return refs.MessageRef{}, fmt.Errorf("publisher: failed to append: %w", err)
	}

	return *msgRef, nil
}

func (p *Publisher) PublishJSON(content map[string]interface{}) (refs.MessageRef, error) {
	data, err := json.Marshal(content)
	if err != nil {
		return refs.MessageRef{}, err
	}
	return p.Publish(data)
}

func (p *Publisher) FeedRef() refs.FeedRef {
	return p.feed
}

func (p *Publisher) Seq() (int64, error) {
	return p.log.Seq()
}

func DeriveKeyPair(masterSeed []byte, did string) (*keys.KeyPair, refs.FeedRef, error) {
	mac := hmac.New(sha256.New, masterSeed)
	mac.Write([]byte(did))
	seed := mac.Sum(nil)

	kp := keys.FromSeed(*(*[32]byte)(seed[:32]))

	pub := kp.Public()
	feedRef, err := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)
	if err != nil {
		return nil, refs.FeedRef{}, err
	}

	return kp, *feedRef, nil
}

func VerifyMessage(sigMsg []byte, sig []byte, pubKey []byte) bool {
	if len(sig) != ed25519.SignatureSize || len(pubKey) != 32 {
		return false
	}
	return ed25519.Verify(pubKey, sigMsg, sig)
}

func HashMessage(data []byte) []byte {
	return legacy.HashMessage(data)
}

func PubKeyToFeedRef(pubKey []byte) (string, error) {
	ref, err := refs.NewFeedRef(pubKey, refs.RefAlgoFeedSSB1)
	if err != nil {
		return "", err
	}
	return ref.String(), nil
}

func FeedRefToPubKey(refStr string) ([]byte, error) {
	ref, err := refs.ParseFeedRef(refStr)
	if err != nil {
		return nil, err
	}
	return ref.PubKey(), nil
}

func (p *Publisher) PublishPrivate(content interface{}, recipientFeed string) (refs.MessageRef, error) {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return refs.MessageRef{}, fmt.Errorf("marshal content: %w", err)
	}

	wrapped, err := crypto.WrapContentForDM(contentJSON, recipientFeed)
	if err != nil {
		return refs.MessageRef{}, fmt.Errorf("wrap DM content: %w", err)
	}

	recipientPubKey, err := FeedRefToPubKey(recipientFeed)
	if err != nil {
		return refs.MessageRef{}, fmt.Errorf("parse recipient: %w", err)
	}

	if len(recipientPubKey) != 32 {
		return refs.MessageRef{}, fmt.Errorf("invalid recipient pubkey length")
	}

	senderPub, senderPriv := p.keyPair.ToCurve25519()
	var recipientPub [32]byte
	copy(recipientPub[:], recipientPubKey)

	encrypted, err := crypto.EncryptDM(wrapped, senderPub, senderPriv, recipientPub)
	if err != nil {
		return refs.MessageRef{}, fmt.Errorf("encrypt DM: %w", err)
	}

	return p.Publish(encrypted)
}
