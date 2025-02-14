package pdf_test

import (
	"fmt"
	"github.com/atlas-editor/pdf"
)

func Example() {
	fp, _ := pdf.Open("testdata/pdfreference1.0.pdf")
	chars, lines, curves, rects, imgs := 0, 0, 0, 0, 0
	for n := 1; n < fp.NumPage()+1; n++ {
		page := fp.Page(n)
		content := page.Content()
		chars += len(content.Chars)
		lines += len(content.Lines)
		curves += len(content.Curves)
		rects += len(content.Rectangles)
		imgs += len(content.Images)

		if n == 1 {
			for _, ch := range content.Chars[:18] {
				fmt.Print(ch.S)
			}
			fmt.Print("\n\n")
		}
	}

	fmt.Printf("chars=%v\nlines=%v\ncurves=%v\nrectangles=%v\nimages=%v\n==========\ncomponents=%v\n",
		chars, lines, curves, rects, imgs, imgs+rects+chars+lines+curves)

	//Output:
	//PDFReferenceManual
	//
	//chars=289744
	//lines=599
	//curves=634
	//rectangles=8251
	//images=15
	//==========
	//components=299243
}
