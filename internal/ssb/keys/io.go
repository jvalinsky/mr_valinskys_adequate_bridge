package keys

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type SecretFile struct {
	Curve   string       `json:"curve"`
	ID      refs.FeedRef `json:"id"`
	Private string       `json:"private"`
	Public  string       `json:"public"`
}

const SecretPerms = 0600

func Load(path string) (*KeyPair, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keys: failed to read secret file: %w", err)
	}

	if err := checkPerms(path); err != nil {
		return nil, fmt.Errorf("keys: insecure permissions on secret file: %w", err)
	}

	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		if len(line) > 0 && line[0] == '#' {
			continue
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			return ParseSecret(bytes.NewReader(trimmed))
		}
	}

	return nil, fmt.Errorf("keys: no valid secret data found")
}

func checkPerms(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	perms := info.Mode().Perm()
	if perms != SecretPerms {
		if err := os.Chmod(path, SecretPerms); err != nil {
			return fmt.Errorf("keys: failed to fix permissions: %w", err)
		}
	}
	return nil
}

func Save(kp *KeyPair, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("keys: secret file already exists: %s", path)
	}

	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil && !os.IsExist(err) {
			return fmt.Errorf("keys: failed to create directory: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, SecretPerms)
	if err != nil {
		return fmt.Errorf("keys: failed to create secret file: %w", err)
	}
	defer f.Close()

	if err := EncodeSecret(kp, f); err != nil {
		return fmt.Errorf("keys: failed to write secret: %w", err)
	}

	return f.Close()
}

func ParseSecret(r io.Reader) (*KeyPair, error) {
	var secret SecretFile
	if err := json.NewDecoder(r).Decode(&secret); err != nil {
		return nil, fmt.Errorf("keys: failed to decode secret JSON: %w", err)
	}

	if secret.Curve != "ed25519" {
		return nil, fmt.Errorf("keys: unsupported curve: %s", secret.Curve)
	}

	pubKey, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(secret.Public, ".ed25519"))
	if err != nil {
		return nil, fmt.Errorf("keys: failed to decode public key: %w", err)
	}

	privKey, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(secret.Private, ".ed25519"))
	if err != nil {
		return nil, fmt.Errorf("keys: failed to decode private key: %w", err)
	}

	if len(pubKey) != 32 || len(privKey) != 64 {
		return nil, fmt.Errorf("keys: invalid key lengths")
	}

	seed := make([]byte, 32)
	copy(seed, privKey)

	return FromSeed(*(*[32]byte)(seed)), nil
}

func EncodeSecret(kp *KeyPair, w io.Writer) error {
	pub := kp.Public()
	secret := SecretFile{
		Curve:   "ed25519",
		ID:      kp.FeedRef(),
		Private: base64.StdEncoding.EncodeToString(kp.Private()) + ".ed25519",
		Public:  base64.StdEncoding.EncodeToString(pub[:]) + ".ed25519",
	}

	return json.NewEncoder(w).Encode(secret)
}
