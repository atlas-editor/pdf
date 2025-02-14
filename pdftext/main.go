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
	"sort"
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

	chars := content.Chars

	// Sort by Y coordinate and normalize.
	const nudge = 1
	sort.Sort(pdf.TextVertical(chars))
	old := -100000.0
	for i, c := range chars {
		if c.Y != old && math.Abs(old-c.Y) < nudge {
			chars[i].Y = old
		} else {
			old = c.Y
		}
	}

	// Sort by Y coordinate, breaking ties with X.
	// This will bring letters in a single word together.
	sort.Sort(pdf.TextVertical(chars))

	buf := strings.Builder{}
	// Loop over chars.
	for i := 0; i < len(chars); {
		// Find all chars on line.
		j := i + 1
		for j < len(chars) && chars[j].Y == chars[i].Y {
			j++
		}

		var end float64
		// Split line into words (really, phrases).
		for k := i; k < j; {
			ck := &chars[k]
			buf.WriteString(ck.S)
			end = ck.X + ck.W
			charSpace := ck.FontSize / 6
			wordSpace := ck.FontSize * 2 / 3
			l := k + 1
			for l < j {
				// Grow word.
				cl := &chars[l]
				if sameFont(cl.Font, ck.Font) && math.Abs(cl.FontSize-ck.FontSize) < 0.1 && cl.X <= end+charSpace {
					buf.WriteString(cl.S)
					end = cl.X + cl.W
					l++
					continue
				}
				// Add space to phrase before next word.
				if sameFont(cl.Font, ck.Font) && math.Abs(cl.FontSize-ck.FontSize) < 0.1 && cl.X <= end+wordSpace {
					buf.WriteString(" " + cl.S)
					end = cl.X + cl.W
					l++
					continue
				}
				break
			}
			k = l
		}
		buf.WriteByte('\n')
		i = j
	}

	return buf.String()
}

func sameFont(f1, f2 string) bool {
	f1 = strings.TrimSuffix(f1, ",Italic")
	f1 = strings.TrimSuffix(f1, "-Italic")
	f2 = strings.TrimSuffix(f1, ",Italic")
	f2 = strings.TrimSuffix(f1, "-Italic")
	return strings.TrimSuffix(f1, ",Italic") == strings.TrimSuffix(f2, ",Italic") ||
		f1 == "Symbol" || f2 == "Symbol" || f1 == "TimesNewRoman" || f2 == "TimesNewRoman"
}
