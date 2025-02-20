// Pdfvis visualizes the layout of a PDF file, optionally specifying a page number.
// If no page number is provided, it visualizes all pages. The output is a simplified representation
// of the page layout, rendering rectangles, lines, general curves and only the bounding boxes of texts.
package main

import (
	"flag"
	"fmt"
	"github.com/atlas-editor/pdf"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

var (
	pagenum = flag.Int("p", -1, "page to visualise")
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: pdfvis [-p pagenum] file\n")
	os.Exit(2)
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("pdfvis: ")

	flag.Usage = usage
	flag.Parse()
	if flag.NArg() != 1 {
		usage()
	}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	page := *pagenum

	st, err := f.Stat()
	if err != nil {
		log.Fatal(err)
	}
	fp, err := pdf.NewReader(f, st.Size())
	if err != nil {
		log.Fatal(err)
	}
	defer fp.Close()

	switch page {
	case -1:
		for n := 1; n < fp.NumPage()+1; n++ {
			img := render(fp, n)
			save(img, fmt.Sprintf("page%d.png", n))
		}
	default:
		if page < 1 || page > fp.NumPage() {
			log.Fatal("page out of bounds")
		}
		img := render(fp, page)
		save(img, fmt.Sprintf("page%d.png", page))
	}

}

func save(img *image.RGBA, filename string) {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	f, err := os.Create(filepath.Join(wd, filename))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	err = png.Encode(f, img)
	if err != nil {
		log.Fatal(err)
	}
}

func render(fp *pdf.Reader, p int) *image.RGBA {
	page := fp.Page(p)
	_, _, w, h := page.MediaBox()
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: image.White}, image.Point{}, draw.Src)

	content := page.Content()

	// texts
	for _, t := range content.Chars {
		for _, pts := range [][]pdf.Point{
			{{t.X, t.Y}, {t.X + t.W, t.Y}},
			{{t.X + t.W, t.Y}, {t.X + t.W, t.Y + t.FontSize}},
			{{t.X + t.W, t.Y + t.FontSize}, {t.X, t.Y + t.FontSize}},
			{{t.X, t.Y + t.FontSize}, {t.X, t.Y}},
		} {
			drawLine(img, pts[0], pts[1], color.Black)
		}
	}

	// rectangles
	for _, re := range content.Rectangles {
		for _, pts := range [][]pdf.Point{
			{{re.X, re.Y}, {re.X + re.W, re.Y}},
			{{re.X + re.W, re.Y}, {re.X + re.W, re.Y + re.H}},
			{{re.X + re.W, re.Y + re.H}, {re.X, re.Y + re.H}},
			{{re.X, re.Y + re.H}, {re.X, re.Y}},
		} {
			drawLine(img, pts[0], pts[1], color.RGBA{255, 0, 0, 255})
		}
	}

	// lines
	for _, l := range content.Lines {
		drawLine(img, l.X, l.Y, color.RGBA{0, 255, 0, 255})
	}

	// general curves
	for _, curve := range content.Curves {
		startPt := pdf.Point{}
		currPt := pdf.Point{}
	SegmentLoop:
		for _, s := range curve.Segments {
			var p0, p1, p2, p3 pdf.Point
			switch s.Type {
			case pdf.M:
				x, y := s.Parameters[0], s.Parameters[1]
				startPt = pdf.Point{x, y}
				currPt = pdf.Point{x, y}
				continue
			case pdf.L:
				newPt := pdf.Point{s.Parameters[0], s.Parameters[1]}
				drawLine(img, currPt, newPt, color.RGBA{0, 0, 255, 255})
				currPt = newPt
				continue
			case pdf.H:
				drawLine(img, currPt, startPt, color.RGBA{0, 0, 255, 255})
				break SegmentLoop
			case pdf.C:
				p0 = currPt
				x1, y1, x2, y2, x3, y3 := s.Parameters[0], s.Parameters[1], s.Parameters[2], s.Parameters[3], s.Parameters[4], s.Parameters[5]
				p1 = pdf.Point{x1, y1}
				p2 = pdf.Point{x2, y2}
				p3 = pdf.Point{x3, y3}
			case pdf.V:
				p0 = currPt
				x2, y2, x3, y3 := s.Parameters[0], s.Parameters[1], s.Parameters[2], s.Parameters[3]
				p1 = p0
				p2 = pdf.Point{x2, y2}
				p3 = pdf.Point{x3, y3}
			case pdf.Y:
				p0 = currPt
				x1, y1, x3, y3 := s.Parameters[0], s.Parameters[1], s.Parameters[2], s.Parameters[3]
				p1 = pdf.Point{x1, y1}
				p2 = pdf.Point{x3, y3}
				p3 = p2
			}
			currPt = p3
			pts := cubicBezier(p0, p1, p2, p3, 500)
			drawPoints(img, pts, color.RGBA{0, 0, 255, 255})
		}
	}

	// images
	for _, im := range content.Images {
		for _, pts := range [][]pdf.Point{
			{im.P0, im.P1},
			{im.P1, im.P2},
			{im.P2, im.P3},
			{im.P3, im.P0},
		} {
			drawLine(img, pts[0], pts[1], color.RGBA{241, 196, 15, 255})
		}
	}

	// flip image via y-axis, pdfs have origin in bottom left corner
	for y := 0; y < img.Bounds().Dy()/2; y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			c1 := img.At(x, y)
			c2 := img.At(x, img.Bounds().Dy()-y-1)
			img.Set(x, y, c2)
			img.Set(x, img.Bounds().Dy()-y-1, c1)
		}
	}

	return img
}

func drawPoints(img *image.RGBA, pts []pdf.Point, c color.Color) {
	for _, point := range pts {
		x, y := point.X, point.Y
		img.Set(int(math.Round(x)), int(math.Round(y)), c)
	}
}

func drawLine(img *image.RGBA, p1 pdf.Point, p2 pdf.Point, c color.Color) {
	x1, y1 := p1.X, p1.Y
	x2, y2 := p2.X, p2.Y
	dx := x2 - x1
	dy := y2 - y1
	steps := int(math.Max(math.Abs(dx), math.Abs(dy))) * 2

	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := x1 + t*dx
		y := y1 + t*dy
		img.Set(int(math.Round(x)), int(math.Round(y)), c)
	}
}

func cubicBezier(p0, p1, p2, p3 pdf.Point, numPoints int) []pdf.Point {
	pts := make([]pdf.Point, numPoints)
	for i := 0; i < numPoints; i++ {
		t := float64(i) / float64(numPoints-1)
		x := (1-t)*(1-t)*(1-t)*p0.X + 3*(1-t)*(1-t)*t*p1.X + 3*(1-t)*t*t*p2.X + t*t*t*p3.X
		y := (1-t)*(1-t)*(1-t)*p0.Y + 3*(1-t)*(1-t)*t*p1.Y + 3*(1-t)*t*t*p2.Y + t*t*t*p3.Y
		pts[i] = pdf.Point{x, y}
	}
	return pts
}
