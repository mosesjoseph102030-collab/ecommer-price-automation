package main

// Generates the neutral hero placeholder image.
//
// The spec (file.md) calls for a licensed photograph of a small business owner
// at public/images/hero-store-owner.webp. That photograph cannot be produced
// here and must not be faked, so this writes a dark neutral placeholder at the
// same path. It is deliberately quiet: the spec requires the overlay to carry
// the headline, so anything busy behind the text would be a contrast risk.
//
// Replace this file with the licensed photograph before launch. Nothing else
// needs to change, because the CSS already treats a missing image as a plain
// colour fallback.

import (
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
)

const (
	width  = 1920
	height = 1080
)

func main() {
	target := filepath.Join("apps", "web-public", "public", "images", "hero-store-owner.jpg")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		panic(err)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// A very low-amplitude diagonal wash. The amplitude stays small on
			// purpose: the overlay gradient darkens the left 54% heavily, and a
			// stronger pattern would fight the headline for attention.
			t := (float64(x)/float64(width) + float64(y)/float64(height)) / 2
			wash := 10 * math.Sin(t*math.Pi*2)
			vignette := 14 * (1 - math.Abs(float64(x)/float64(width)-0.72))

			base := 26 + wash - vignette
			img.Set(x, y, color.RGBA{
				R: uint8(clamp(base+2)),
				G: uint8(clamp(base+8)),
				B: uint8(clamp(base+10)),
				A: 255,
			})
		}
	}

	file, err := os.Create(target)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	// Quality 82 keeps the file small; the overlay means fine detail is wasted.
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 82}); err != nil {
		panic(err)
	}
	info, _ := os.Stat(target)
	println("wrote", target, info.Size(), "bytes")
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
