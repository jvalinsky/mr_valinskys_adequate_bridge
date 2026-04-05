package keys

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/skip2/go-qrcode"
)

type QRFormat string

const (
	QRFormatPNG    QRFormat = "png"
	QRFormatBase64 QRFormat = "base64"
)

func ExportQR(kp *KeyPair, format QRFormat) ([]byte, error) {
	content := kp.ToBase64JSON()

	switch format {
	case QRFormatBase64:
		png, err := qrcode.Encode(content, qrcode.Medium, 256)
		if err != nil {
			return nil, fmt.Errorf("generate PNG: %w", err)
		}
		return []byte(base64.StdEncoding.EncodeToString(png)), nil

	case QRFormatPNG:
		png, err := qrcode.Encode(content, qrcode.Medium, 256)
		if err != nil {
			return nil, fmt.Errorf("generate PNG: %w", err)
		}
		return png, nil

	default:
		return nil, fmt.Errorf("unsupported QR format: %s (supported: png, base64)", format)
	}
}

func ImportQR(data []byte, format QRFormat) (*KeyPair, error) {
	var content string

	switch format {
	case QRFormatBase64:
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("decode base64: %w", err)
		}
		content = string(decoded)

	case QRFormatPNG:
		content = string(data)

	default:
		return nil, fmt.Errorf("unsupported QR format: %s", format)
	}

	return ParseSecret(strings.NewReader(content))
}
