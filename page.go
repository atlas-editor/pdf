// Copyright 2014 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"log/slog"
	"strings"
)

// A Page represent a single page in a PDF file.
// The methods interpret a Page dictionary stored in V.
type Page struct {
	V Value
}

// Page returns the page for the given page number.
// Page numbers are indexed starting at 1, not 0.
// If the page is not found, Page returns a Page with p.V.IsNull().
func (r *Reader) Page(num int) Page {
	num-- // now 0-indexed
	page := r.Trailer().Key("Root").Key("Pages")
Search:
	for page.Key("Type").Name() == "Pages" {
		count := int(page.Key("Count").Int64())
		if count < num {
			return Page{}
		}
		kids := page.Key("Kids")
		for i := 0; i < kids.Len(); i++ {
			kid := kids.Index(i)
			if kid.Key("Type").Name() == "Pages" {
				c := int(kid.Key("Count").Int64())
				if num < c {
					page = kid
					continue Search
				}
				num -= c
				continue
			}
			if kid.Key("Type").Name() == "Page" {
				if num == 0 {
					return Page{kid}
				}
				num--
			}
		}
		break
	}
	return Page{}
}

// NumPage returns the number of pages in the PDF file.
func (r *Reader) NumPage() int {
	return int(r.Trailer().Key("Root").Key("Pages").Key("Count").Int64())
}

func (p Page) findInherited(key string) Value {
	for v := p.V; !v.IsNull(); v = v.Key("Parent") {
		if r := v.Key(key); !r.IsNull() {
			return r
		}
	}
	return Value{}
}

func (p Page) MediaBox() (float64, float64, float64, float64) {
	if obj, ok := p.findInherited("MediaBox").data.(array); ok && len(obj) == 4 {
		vals := []float64{}
		for _, o := range obj {
			switch o.(type) {
			case float64:
				vals = append(vals, o.(float64))
			case int64:
				vals = append(vals, float64(o.(int64)))
			default:
				return 0, 0, 0, 0
			}
		}
		return vals[0], vals[1], vals[2], vals[3]
	}
	return 0, 0, 0, 0
}

// Resources returns the resources dictionary associated with the page.
func (p Page) Resources() Value {
	return p.findInherited("Resources")
}

// Fonts returns a list of the fonts associated with the page.
func (p Page) Fonts() []string {
	return p.Resources().Key("Font").Keys()
}

// Font returns the font with the given name associated with the page.
func (p Page) Font(name string) Font {
	return Font{p.Resources().Key("Font").Key(name)}
}

// A Font represent a font in a PDF file.
// The methods interpret a Font dictionary stored in V.
type Font struct {
	V Value
}

// BaseFont returns the font's name (BaseFont property).
func (f Font) BaseFont() string {
	return f.V.Key("BaseFont").Name()
}

// FirstChar returns the code point of the first character in the font.
func (f Font) FirstChar() int {
	return int(f.V.Key("FirstChar").Int64())
}

// LastChar returns the code point of the last character in the font.
func (f Font) LastChar() int {
	return int(f.V.Key("LastChar").Int64())
}

// Widths returns the widths of the glyphs in the font.
// In a well-formed PDF, len(f.Widths()) == f.LastChar()+1 - f.FirstChar().
func (f Font) Widths() []float64 {
	x := f.V.Key("Widths")
	var out []float64
	for i := 0; i < x.Len(); i++ {
		out = append(out, x.Index(i).Float64())
	}
	return out
}

// Width returns the width of the given code point.
func (f Font) Width(code int) float64 {
	if corewidths, ok := Core14Widths[f.BaseFont()]; ok {
		return corewidths[code]
	}
	first := f.FirstChar()
	last := f.LastChar()
	if code < first || last < code {
		return 0
	}
	return f.V.Key("Widths").Index(code - first).Float64()
}

// Encoder returns the encoding between font code point sequences and UTF-8.
func (f Font) Encoder() TextEncoding {
	enc := f.V.Key("Encoding")
	switch enc.Kind() {
	case Name:
		switch enc.Name() {
		case "WinAnsiEncoding":
			return &byteEncoder{&winAnsiEncoding}
		case "MacRomanEncoding":
			return &byteEncoder{&macRomanEncoding}
		case "Identity-H":
			// TODO: Should be big-endian UCS-2 decoder
			return &nopEncoder{}
		default:
			println("unknown encoding", enc.Name())
			return &nopEncoder{}
		}
	case Dict:
		return &dictEncoder{enc.Key("Differences")}
	case Null:
		// ok, try ToUnicode
	default:
		println("unexpected encoding", enc.String())
		return &nopEncoder{}
	}

	toUnicode := f.V.Key("ToUnicode")
	if toUnicode.Kind() == Dict {
		m := readCmap(toUnicode)
		if m == nil {
			return &nopEncoder{}
		}
		return m
	}

	return &byteEncoder{&pdfDocEncoding}
}

type dictEncoder struct {
	v Value
}

func (e *dictEncoder) Decode(raw string) (text string) {
	r := make([]rune, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		ch := rune(raw[i])
		n := -1
		for j := 0; j < e.v.Len(); j++ {
			x := e.v.Index(j)
			if x.Kind() == Integer {
				n = int(x.Int64())
				continue
			}
			if x.Kind() == Name {
				if int(raw[i]) == n {
					r := nameToRune[x.Name()]
					if r != 0 {
						ch = r
						break
					}
				}
				n++
			}
		}
		r = append(r, ch)
	}
	return string(r)
}

// Content describes the basic content on a page: the text and any drawn rectangles.
type Content struct {
	Chars      []Char
	Rectangles []Rectangle
	Lines      []Line
	Curves     []Curve
	Images     []Image
}

func (c *Content) extend(c2 Content) {
	c.Chars = append(c.Chars, c2.Chars...)
	c.Rectangles = append(c.Rectangles, c2.Rectangles...)
	c.Lines = append(c.Lines, c2.Lines...)
	c.Curves = append(c.Curves, c2.Curves...)
	c.Images = append(c.Images, c2.Images...)
}

type gstate struct {
	Tc    float64
	Tw    float64
	Th    float64
	Tl    float64
	Tf    Font
	Tfs   float64
	Tmode int
	Trise float64
	Tm    matrix
	Tlm   matrix
	Trm   matrix
	CTM   matrix
}

// Content returns the page's content.
func (p Page) Content() Content {
	obj := p.V.Key("Contents")

	content := Content{}
	gstack := []gstate{}
	switch obj.Kind() {
	case Stream:
		content, _ = interpretContentStream(p, obj, gstack)
	case Array:
		for i := 0; i < obj.Len(); i++ {
			val := obj.Index(i)
			part := Content{}
			if val.Kind() == Stream {
				part, gstack = interpretContentStream(p, val, gstack)
				content.extend(part)
			} else {
				panic("`Contents` array must only contain streams")
			}
		}
	default:
		panic("`Contents` must be a stream or an array of streams")
	}

	return content
}

func interpretContentStream(p Page, strm Value, gstack []gstate) (Content, []gstate) {
	var enc TextEncoding = &nopEncoder{}

	var g = gstate{
		Th:  1,
		CTM: ident,
	}

	var chars []Char
	showChar := func(s string) {
		n := 0
		for _, ch := range enc.Decode(s) {
			Trm := matrix{{g.Tfs * g.Th, 0, 0}, {0, g.Tfs, 0}, {0, g.Trise, 1}}.mul(g.Tm).mul(g.CTM)
			w0 := g.Tf.Width(int(s[n]))
			n++
			if ch != ' ' {
				f := g.Tf.BaseFont()
				if i := strings.Index(f, "+"); i >= 0 {
					f = f[i+1:]
				}
				chars = append(chars, Char{f, Trm[0][0], Trm[2][0], Trm[2][1], w0 / 1000 * Trm[0][0], string(ch)})
			}
			tx := w0/1000*g.Tfs + g.Tc
			if ch == ' ' {
				tx += g.Tw
			}
			tx *= g.Th
			g.Tm = matrix{{1, 0, 0}, {0, 1, 0}, {tx, 0, 1}}.mul(g.Tm)
		}
	}

	var curves []Curve
	var rects []Rectangle
	var lines []Line
	constructPath := func(path []Segment, mode string) {
		if len(path) == 0 {
			return
		}
		for sp := range splitPath(path) {
			tp := []Segment{}
			for _, s := range sp {
				ps := []float64{}
				for i := 0; i < len(s.Parameters)-1; i += 2 {
					pt := Point{s.Parameters[i], s.Parameters[i+1]}
					tpt := g.CTM.apply(pt)
					ps = append(ps, tpt.X, tpt.Y)
				}
				tp = append(tp, Segment{s.Type, ps})
			}

			if line, ok := isLine(tp); ok {
				lines = append(lines, line)
			} else if rect, ok1 := isRectangle(tp); ok1 {
				rects = append(rects, rect)
			} else {
				curves = append(curves, Curve{tp})
			}
		}
	}

	var images []Image
	var curpath []Segment
	Interpret(strm, func(stk *Stack, op string) {
		n := stk.Len()
		args := make([]Value, n)
		for i := n - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		switch op {
		default:
			slog.Debug(fmt.Sprintf("unhandled op=%v with args=%v", op, args))
			return

		case "cm": // update g.CTM
			if len(args) != 6 {
				panic("bad g.Tm")
			}
			var m matrix
			for i := 0; i < 6; i++ {
				m[i/2][i%2] = args[i].Float64()
			}
			m[2][2] = 1
			g.CTM = m.mul(g.CTM)

		case "gs": // set parameters from graphics state resource
			gs := p.Resources().Key("ExtGState").Key(args[0].Name())
			font := gs.Key("Font")
			if font.Kind() == Array && font.Len() == 2 {
				//fmt.Println("FONT", font)
			}

		case "f", "F": // fill
			constructPath(curpath, "f")
			curpath = nil
		case "f*": // fill
			constructPath(curpath, "f*")
			curpath = nil
		case "s", "S": // close and stroke
			constructPath(append(curpath, Segment{"h", []float64{}}), "S")
			curpath = nil
		case "b": // close, fill and stroke
			constructPath(append(curpath, Segment{"h", []float64{}}), "B")
			curpath = nil
		case "b*": // close, fill and stroke
			constructPath(append(curpath, Segment{"h", []float64{}}), "B*")
			curpath = nil
		case "B": // fill and stroke
			constructPath(curpath, "B")
			curpath = nil
		case "B*": // fill and stroke
			constructPath(curpath, "B*")
			curpath = nil
		case "n":
			curpath = nil
		case "g": // setgray
		case "m": // moveto
			if len(args) != 2 {
				panic("bad m")
			}
			x, y := args[0].Float64(), args[1].Float64()
			curpath = append(curpath, Segment{"m", []float64{x, y}})
		case "l": // lineto
			if len(args) != 2 {
				panic("bad l")
			}
			x, y := args[0].Float64(), args[1].Float64()
			curpath = append(curpath, Segment{"l", []float64{x, y}})
		case "c": // curves (three control points)
			if len(args) != 6 {
				panic("bad c")
			}
			x1, y1, x2, y2, x3, y3 := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64(), args[4].Float64(), args[5].Float64()
			curpath = append(curpath, Segment{"c", []float64{x1, y1, x2, y2, x3, y3}})
		case "v": // curves (initial point replicated)
			if len(args) != 4 {
				panic("bad v")
			}
			x2, y2, x3, y3 := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
			curpath = append(curpath, Segment{"v", []float64{x2, y2, x3, y3}})
		case "y": // curves (final point replicated)
			if len(args) != 4 {
				panic("bad y")
			}
			x1, y1, x3, y3 := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
			curpath = append(curpath, Segment{"y", []float64{x1, y1, x3, y3}})
		case "h": // close subpath
			curpath = append(curpath, Segment{"h", []float64{}})

		case "cs": // set colorspace non-stroking
		case "scn": // set color non-stroking

		case "re": // append rectangle to path
			if len(args) != 4 {
				panic("bad re")
			}
			x, y, w, h := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
			curpath = append(curpath,
				Segment{"m", []float64{x, y}},
				Segment{"l", []float64{x + w, y}},
				Segment{"l", []float64{x + w, y + h}},
				Segment{"l", []float64{x, y + h}},
				Segment{"h", []float64{}},
			)

		case "q": // save graphics state
			gstack = append(gstack, g)

		case "Q": // restore graphics state
			m := len(gstack) - 1
			if m >= 0 {
				g = gstack[m]
				gstack = gstack[:m]
			}

		case "BT": // begin text (reset text matrix and line matrix)
			g.Tm = ident
			g.Tlm = g.Tm

		case "ET": // end text

		case "T*": // move to start of next line
			x := matrix{{1, 0, 0}, {0, 1, 0}, {0, -g.Tl, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm

		case "Tc": // set character spacing
			if len(args) != 1 {
				panic("bad g.Tc")
			}
			g.Tc = args[0].Float64()

		case "TD": // move text position and set leading
			if len(args) != 2 {
				panic("bad Td")
			}
			g.Tl = -args[1].Float64()
			fallthrough
		case "Td": // move text position
			if len(args) != 2 {
				panic("bad Td")
			}
			tx := args[0].Float64()
			ty := args[1].Float64()
			x := matrix{{1, 0, 0}, {0, 1, 0}, {tx, ty, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm

		case "Tf": // set text font and size
			if len(args) != 2 {
				panic("bad TL")
			}
			f := args[0].Name()
			g.Tf = p.Font(f)
			enc = g.Tf.Encoder()
			if enc == nil {
				slog.Debug(fmt.Sprintf("no cmap for %v", f))
				enc = &nopEncoder{}
			}
			g.Tfs = args[1].Float64()

		case "\"": // set spacing, move to next line, and show text
			if len(args) != 3 {
				panic("bad \" operator")
			}
			g.Tw = args[0].Float64()
			g.Tc = args[1].Float64()
			args = args[2:]
			fallthrough
		case "'": // move to next line and show text
			if len(args) != 1 {
				panic("bad ' operator")
			}
			x := matrix{{1, 0, 0}, {0, 1, 0}, {0, -g.Tl, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm
			fallthrough
		case "Tj": // show text
			if len(args) != 1 {
				panic("bad Tj operator")
			}
			showChar(args[0].RawString())

		case "TJ": // show text, allowing individual glyph positioning
			v := args[0]
			for i := 0; i < v.Len(); i++ {
				x := v.Index(i)
				if x.Kind() == String {
					showChar(x.RawString())
				} else {
					tx := -x.Float64() / 1000 * g.Tfs * g.Th
					g.Tm = matrix{{1, 0, 0}, {0, 1, 0}, {tx, 0, 1}}.mul(g.Tm)
				}
			}

		case "TL": // set text leading
			if len(args) != 1 {
				panic("bad TL")
			}
			g.Tl = args[0].Float64()

		case "Tm": // set text matrix and line matrix
			if len(args) != 6 {
				panic("bad g.Tm")
			}
			var m matrix
			for i := 0; i < 6; i++ {
				m[i/2][i%2] = args[i].Float64()
			}
			m[2][2] = 1
			g.Tm = m
			g.Tlm = m

		case "Tr": // set text rendering mode
			if len(args) != 1 {
				panic("bad Tr")
			}
			g.Tmode = int(args[0].Int64())

		case "Ts": // set text rise
			if len(args) != 1 {
				panic("bad Ts")
			}
			g.Trise = args[0].Float64()

		case "Tw": // set word spacing
			if len(args) != 1 {
				panic("bad g.Tw")
			}
			g.Tw = args[0].Float64()

		case "Tz": // set horizontal text scaling
			if len(args) != 1 {
				panic("bad Tz")
			}
			g.Th = args[0].Float64() / 100
		case "Do":
			if len(args) != 1 {
				panic("bad Do")
			}

			if imstrm := p.Resources().Key("XObject").Key(args[0].Name()); imstrm.Key("Subtype").Name() == "Image" {
				P0 := g.CTM.apply(Point{0, 0})
				P1 := g.CTM.apply(Point{1, 0})
				P2 := g.CTM.apply(Point{1, 1})
				P3 := g.CTM.apply(Point{0, 1})
				im := Image{P0, P1, P2, P3, imstrm.Key("Filter").String(), imstrm.Reader()}
				images = append(images, im)
			}
		}
	})
	return Content{chars, rects, lines, curves, images}, gstack
}

// TextVertical implements sort.Interface for sorting
// a slice of Text values in vertical order, top to bottom,
// and then left to right within a line.
type TextVertical []Char

func (x TextVertical) Len() int      { return len(x) }
func (x TextVertical) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x TextVertical) Less(i, j int) bool {
	if x[i].Y != x[j].Y {
		return x[i].Y > x[j].Y
	}
	return x[i].X < x[j].X
}

// TextHorizontal implements sort.Interface for sorting
// a slice of Text values in horizontal order, left to right,
// and then top to bottom within a column.
type TextHorizontal []Char

func (x TextHorizontal) Len() int      { return len(x) }
func (x TextHorizontal) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x TextHorizontal) Less(i, j int) bool {
	if x[i].X != x[j].X {
		return x[i].X < x[j].X
	}
	return x[i].Y > x[j].Y
}

// An Outline is a tree describing the outline (also known as the table of contents)
// of a document.
type Outline struct {
	Title string    // title for this element
	Child []Outline // child elements
}

// Outline returns the document outline.
// The Outline returned is the root of the outline tree and typically has no Title itself.
// That is, the children of the returned root are the top-level entries in the outline.
func (r *Reader) Outline() Outline {
	return buildOutline(r.Trailer().Key("Root").Key("Outlines"))
}

func buildOutline(entry Value) Outline {
	var x Outline
	x.Title = entry.Key("Title").Text()
	for child := entry.Key("First"); child.Kind() == Dict; child = child.Key("Next") {
		x.Child = append(x.Child, buildOutline(child))
	}
	return x
}
