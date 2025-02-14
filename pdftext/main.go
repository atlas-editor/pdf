// Pdftext extracts text from a PDF file, optionally specifying a page number.
// If no page number is provided, it extracts text from all pages.
package main

import (
	"flag"
	"fmt"
	"github.com/atlas-editor/pdf"
	"log"
	"math"
	"os"
	"slices"
	"strings"
)

var (
	pagenum = flag.Int("p", -1, "page to process")
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: pdftext [-p pagenum] file\n")
	os.Exit(2)
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("pdftext: ")

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
			fmt.Println(extract(fp, n))
		}
	default:
		if page < 1 || page > fp.NumPage() {
			log.Fatal("page out of bounds")
		}
		fmt.Println(extract(fp, page))
	}
}

func extract(fp *pdf.Reader, p int) string {
	page := fp.Page(p)
	content := page.Content()
	if len(content.Chars) == 0 {
		return ""
	}

	slices.SortStableFunc(content.Chars, func(a, b pdf.Char) int {
		if a.Y < b.Y {
			return 1
		}
		if a.Y == b.Y {
			if a.X < b.X {
				return -1
			}
			if a.X == b.X {
				return 0
			}
			return 1
		}
		return -1
	})

	buf := strings.Builder{}
	y := content.Chars[0].Y
	for _, t := range content.Chars {
		if math.Abs(t.Y-y) > 1e-6*math.Max(math.Abs(t.Y), math.Abs(y)) {
			buf.WriteByte('\n')
		}
		buf.WriteString(t.S)
		y = t.Y
	}
	return buf.String()
}
