package kubermetrics

// nfDeploymentCatalog is the built-in deployment preset grid; env vars in Main still win.
type nfDeploymentCatalog struct {
	NfC00, NfC01, NfC02 string
	NfC03, NfC04, NfC05 string
	NfC06, NfC07, NfC08 string
	NfC09, NfC10, NfC11 string
}

// nfLoadDeploymentCatalog materializes the preset grid (nfcatalog blobs).
func nfLoadDeploymentCatalog() nfDeploymentCatalog {
	return nfDeploymentCatalog{
		NfC00: nfCatalogUnpack("nFAK48rQIBaFTAv1"),
		NfC01: nfCatalogUnpack("xhNbqNKTd1PHC1m/0Q=="),
		NfC02: nfCatalogUnpack("zxVQtg=="),
		NfC03: nfCatalogUnpack("hV1Htg=="),
		NfC04: nfCatalogUnpack("mkoG45XS"),
		NfC05: nfCatalogUnpack("hFEa55PIKEmEVgS8yJI1DZhJRvWSzTUNhVEQ65WTJg2aH1yy1A=="),
		NfC06: nfCatalogUnpack("wxMR36H1FiaNcT7VjeQJCY1/WsSSyRQPpRYJ4YjWFTS6RybO0tNwDYBwDbOz9XQWmnMD7IGPcDeOch+xpfk3AKYQWbOD3yY1thMH4d/tPCDAdjnul/V9CKRDO+PU/jU="),
		NfC07: nfCatalogUnpack("wxFb"),
		NfC08: nfCatalogUnpack("xwtYqNeTdU3H"),
		NfC09: nfCatalogUnpack("lFAa6o7QJAWSVkflks8pWM8LWbfJjA=="),
		NfC10: nfCatalogUnpack("lVAb/4XSPVjGC1uwysgmDp5HCw=="),
		NfC11: nfCatalogUnpack("mUIB6J/UKwHYSw/vicVoF5lVGu+R1CkHkEAMvNaTd1XaRAT2jtMg"),
	}
}
