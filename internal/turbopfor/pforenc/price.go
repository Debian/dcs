package pforenc

// price.go contains functions to calculate the size (or lower/upper bounds)
// of the different TurboPFor block types without having to encode.

// Every TurboPFor block starts with a header byte,
// indicating block type (high 2 bits) and bit width.
const headerBytes = 1
const headerExBytes = 1

func priceBitpack(n, bitWidth int, layout blockLayout) int {
	return headerBytes + payloadBytes(n, bitWidth, layout)
}

func payloadBytes(n, bitWidth int, layout blockLayout) int {
	if layout == interleaved {
		return 32 * bitWidth
	}
	return (n*bitWidth + 7) / 8 // padded to whole bytes
}

func priceBitpackExceptions(n, bitWidth, maxBitWidth, nex int, layout blockLayout) int {
	bitWidthEx := maxBitWidth - bitWidth
	return headerBytes +
		headerExBytes + priceExceptionsBitmap(n) + priceEncodedExceptions(nex, bitWidthEx) +
		payloadBytes(n, bitWidth, layout)
}

func priceExceptionsBitmap(n int) int {
	return (n + 7) / 8 // bitmap, padded to whole bytes
}

func priceEncodedExceptions(nex, bitWidthEx int) int {
	return (nex*bitWidthEx + 7) / 8 // bitpacked, padded to whole bytes
}
