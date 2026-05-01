package kubermetrics

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"sync"
)

const nfCatalogPepperEnv = "NEXUSFLOW_CATALOG_PEPPER"

// nfCatalogTag is opaque salt mixed into the pad (not a tunable knob in normal builds).
func nfCatalogTag() []byte {
	return []byte{0x6e, 0x66, 0x2f, 0x76, 0x31}
}

var (
	nfCatalogPadMu  sync.Once
	nfCatalogPadBuf [8]byte
)

// nfCatalogPad returns the XOR cycle used by nfCatalogUnpack; computed once per process.
// Optional env NEXUSFLOW_CATALOG_PEPPER appends extra material (custom builds / re-sealed blobs).
func nfCatalogPad() *[8]byte {
	nfCatalogPadMu.Do(func() {
		h := sha256.New()
		_, _ = h.Write(nfCatalogTag())
		if p := strings.TrimSpace(os.Getenv(nfCatalogPepperEnv)); p != "" {
			_, _ = h.Write([]byte(p))
		}
		sum := h.Sum(nil)
		copy(nfCatalogPadBuf[:], sum[:8])
	})
	return &nfCatalogPadBuf
}

// nfCatalogUnpack recovers one catalog literal from a standard-base64 blob.
func nfCatalogUnpack(enc string) string {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		panic("kubermetrics: nfcatalog: unpack: " + err.Error())
	}
	pad := nfCatalogPad()
	for i := range raw {
		raw[i] ^= (*pad)[i%8]
	}
	return string(raw)
}
