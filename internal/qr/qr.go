// Package qr makes QR codes as SVG, for setting up two-step sign-in in an
// authenticator app. The encoder (coding, gf256) is from rsc.io/qr by
// Russ Cox / The Go Authors, BSD licence (see LICENSE), copied here so the
// build needs no extra download.
package qr

import (
	"errors"
	"fmt"
	"strings"

	"apex-billing/internal/qr/coding"
)

// SVG encodes text (level M) as a square SVG image with a 4-module quiet
// zone, black on white.
func SVG(text string) (string, error) {
	var enc coding.Encoding = coding.String(text)
	l := coding.M
	var v coding.Version
	for v = coding.MinVersion; ; v++ {
		if v > coding.MaxVersion {
			return "", errors.New("text too long for a QR code")
		}
		if enc.Bits(v) <= v.DataBytes(l)*8 {
			break
		}
	}
	p, err := coding.NewPlan(v, l, 0)
	if err != nil {
		return "", err
	}
	cc, err := p.Encode(enc)
	if err != nil {
		return "", err
	}
	black := func(x, y int) bool {
		return 0 <= x && x < cc.Size && 0 <= y && y < cc.Size && cc.Bitmap[y*cc.Stride+x/8]&(1<<uint(7-x&7)) != 0
	}
	n := cc.Size + 8
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n, n, n)
	for y := 0; y < cc.Size; y++ {
		for x := 0; x < cc.Size; x++ {
			if black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+4, y+4)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}
