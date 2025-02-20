package pdf

import (
	"fmt"
	"io"
	"strings"
)

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

type Interpreter struct {
	rsrcs    Value
	enc      TextEncoding
	encCache map[string]TextEncoding
	g        gstate
	gstack   []gstate
}

func NewInterpreter(rsrcs Value) *Interpreter {
	return &Interpreter{
		rsrcs:    rsrcs,
		enc:      &nopEncoder{},
		encCache: make(map[string]TextEncoding),
		g:        gstate{Th: 1, CTM: ident},
		gstack:   make([]gstate, 0)}
}

func (interp *Interpreter) InterpretContentStream(strm Value) Content {
	var chars []Char
	var curves []Curve
	var rects []Rectangle
	var lines []Line
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
			//println(fmt.Sprintf("unhandled op=%v with args=%v", op, args))
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
			interp.g.CTM = m.mul(interp.g.CTM)

		case "gs": // set parameters from graphics state resource
			gs := interp.rsrcs.Key("ExtGState").Key(args[0].Name())
			font := gs.Key("Font")
			if font.Kind() == Array && font.Len() == 2 {
				//fmt.Println("FONT", font)
			}

		case "s", "S", "b", "b*": // close and stroke, close fill and stroke
			curpath = append(curpath, Segment{H, []float64{}})
			fallthrough
		case "f", "F", "f*", "B", "B*": // fill, fill and stroke
			l, r, c := interp.constructPath(curpath, op)
			lines = append(lines, l...)
			rects = append(rects, r...)
			curves = append(curves, c...)
			curpath = nil
		case "n":
			curpath = nil
		case "g": // setgray
		case "m": // moveto
			if len(args) != 2 {
				panic("bad m")
			}
			x, y := args[0].Float64(), args[1].Float64()
			curpath = append(curpath, Segment{M, []float64{x, y}})
		case "l": // lineto
			if len(args) != 2 {
				panic("bad l")
			}
			x, y := args[0].Float64(), args[1].Float64()
			curpath = append(curpath, Segment{L, []float64{x, y}})
		case "c": // curves (three control points)
			if len(args) != 6 {
				panic("bad c")
			}
			x1, y1, x2, y2, x3, y3 := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64(), args[4].Float64(), args[5].Float64()
			curpath = append(curpath, Segment{C, []float64{x1, y1, x2, y2, x3, y3}})
		case "v": // curves (initial point replicated)
			if len(args) != 4 {
				panic("bad v")
			}
			x2, y2, x3, y3 := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
			curpath = append(curpath, Segment{V, []float64{x2, y2, x3, y3}})
		case "y": // curves (final point replicated)
			if len(args) != 4 {
				panic("bad y")
			}
			x1, y1, x3, y3 := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
			curpath = append(curpath, Segment{Y, []float64{x1, y1, x3, y3}})
		case "h": // close subpath
			curpath = append(curpath, Segment{H, []float64{}})

		case "cs": // set colorspace non-stroking
		case "scn": // set color non-stroking

		case "re": // append rectangle to path
			if len(args) != 4 {
				panic("bad re")
			}
			x, y, w, h := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
			curpath = append(curpath,
				Segment{M, []float64{x, y}},
				Segment{L, []float64{x + w, y}},
				Segment{L, []float64{x + w, y + h}},
				Segment{L, []float64{x, y + h}},
				Segment{H, []float64{}},
			)

		case "q": // save graphics state
			interp.gstack = append(interp.gstack, interp.g)

		case "Q": // restore graphics state
			m := len(interp.gstack) - 1
			if m >= 0 {
				interp.g = interp.gstack[m]
				interp.gstack = interp.gstack[:m]
			}

		case "BT": // begin text (reset text matrix and line matrix)
			interp.g.Tm = ident
			interp.g.Tlm = interp.g.Tm

		case "ET": // end text

		case "T*": // move to start of next line
			x := matrix{{1, 0, 0}, {0, 1, 0}, {0, -interp.g.Tl, 1}}
			interp.g.Tlm = x.mul(interp.g.Tlm)
			interp.g.Tm = interp.g.Tlm

		case "Tc": // set character spacing
			if len(args) != 1 {
				panic("bad g.Tc")
			}
			interp.g.Tc = args[0].Float64()

		case "TD": // move text position and set leading
			if len(args) != 2 {
				panic("bad Td")
			}
			interp.g.Tl = -args[1].Float64()
			fallthrough
		case "Td": // move text position
			if len(args) != 2 {
				panic("bad Td")
			}
			tx := args[0].Float64()
			ty := args[1].Float64()
			x := matrix{{1, 0, 0}, {0, 1, 0}, {tx, ty, 1}}
			interp.g.Tlm = x.mul(interp.g.Tlm)
			interp.g.Tm = interp.g.Tlm

		case "Tf": // set text font and size
			if len(args) != 2 {
				panic("bad TL")
			}
			f := args[0].Name()
			interp.g.Tf = Font{interp.rsrcs.Key("Font").Key(f)}
			if enc, ok := interp.encCache[f]; ok {
				interp.enc = enc
			} else {
				interp.enc = interp.g.Tf.Encoder()
				interp.encCache[f] = interp.enc
			}
			if interp.enc == nil {
				println(fmt.Sprintf("no cmap for %v", f))
				interp.enc = &nopEncoder{}
			}
			interp.g.Tfs = args[1].Float64()

		case "\"": // set spacing, move to next line, and show text
			if len(args) != 3 {
				panic("bad \" operator")
			}
			interp.g.Tw = args[0].Float64()
			interp.g.Tc = args[1].Float64()
			args = args[2:]
			fallthrough
		case "'": // move to next line and show text
			if len(args) != 1 {
				panic("bad ' operator")
			}
			x := matrix{{1, 0, 0}, {0, 1, 0}, {0, -interp.g.Tl, 1}}
			interp.g.Tlm = x.mul(interp.g.Tlm)
			interp.g.Tm = interp.g.Tlm
			fallthrough
		case "Tj": // show text
			if len(args) != 1 {
				panic("bad Tj operator")
			}
			t := interp.showText(args[0].RawString())
			chars = append(chars, t...)

		case "TJ": // show text, allowing individual glyph positioning
			v := args[0]
			for i := 0; i < v.Len(); i++ {
				x := v.Index(i)
				if x.Kind() == String {
					t := interp.showText(x.RawString())
					chars = append(chars, t...)
				} else {
					tx := -x.Float64() / 1000 * interp.g.Tfs * interp.g.Th
					interp.g.Tm = matrix{{1, 0, 0}, {0, 1, 0}, {tx, 0, 1}}.mul(interp.g.Tm)
				}
			}

		case "TL": // set text leading
			if len(args) != 1 {
				panic("bad TL")
			}
			interp.g.Tl = args[0].Float64()

		case "Tm": // set text matrix and line matrix
			if len(args) != 6 {
				panic("bad g.Tm")
			}
			var m matrix
			for i := 0; i < 6; i++ {
				m[i/2][i%2] = args[i].Float64()
			}
			m[2][2] = 1
			interp.g.Tm = m
			interp.g.Tlm = m

		case "Tr": // set text rendering mode
			if len(args) != 1 {
				panic("bad Tr")
			}
			interp.g.Tmode = int(args[0].Int64())

		case "Ts": // set text rise
			if len(args) != 1 {
				panic("bad Ts")
			}
			interp.g.Trise = args[0].Float64()

		case "Tw": // set word spacing
			if len(args) != 1 {
				panic("bad g.Tw")
			}
			interp.g.Tw = args[0].Float64()

		case "Tz": // set horizontal text scaling
			if len(args) != 1 {
				panic("bad Tz")
			}
			interp.g.Th = args[0].Float64() / 100
		case "Do", "EI":
			if len(args) != 1 {
				panic("bad Do/EI")
			}
			images = append(images, interp.renderImage(args[0]))
		}
	})
	return Content{chars, rects, lines, curves, images}
}

func (interp *Interpreter) showText(s string) []Char {
	var chars []Char
	n := 0
	for _, ch := range interp.enc.Decode(s) {
		Trm := matrix{{interp.g.Tfs * interp.g.Th, 0, 0}, {0, interp.g.Tfs, 0}, {0, interp.g.Trise, 1}}.mul(interp.g.Tm).mul(interp.g.CTM)
		w0 := interp.g.Tf.Width(int(s[n]))
		n++
		if ch != ' ' {
			f := interp.g.Tf.BaseFont()
			if i := strings.Index(f, "+"); i >= 0 {
				f = f[i+1:]
			}
			chars = append(chars, Char{f, Trm[0][0], Trm[2][0], Trm[2][1], w0 / 1000 * Trm[0][0], string(ch)})
		}
		tx := w0/1000*interp.g.Tfs + interp.g.Tc
		if ch == ' ' {
			tx += interp.g.Tw
		}
		tx *= interp.g.Th
		interp.g.Tm = matrix{{1, 0, 0}, {0, 1, 0}, {tx, 0, 1}}.mul(interp.g.Tm)
	}
	return chars
}

func (interp *Interpreter) constructPath(path []Segment, mode string) ([]Line, []Rectangle, []Curve) {
	var lines []Line
	var rects []Rectangle
	var curves []Curve
	if len(path) == 0 {
		return lines, rects, curves
	}
	for sp := range splitPath(path) {
		var tp []Segment
		for _, s := range sp {
			var ps []float64
			for i := 0; i < len(s.Parameters)-1; i += 2 {
				pt := Point{s.Parameters[i], s.Parameters[i+1]}
				tpt := interp.g.CTM.apply(pt)
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
	return lines, rects, curves
}

func (interp *Interpreter) renderImage(im Value) Image {
	var data io.ReadCloser
	var filters []FilterType
	var imstrm Value
	switch im.Kind() {
	case Name:
		if obj := interp.rsrcs.Key("XObject").Key(im.Name()); obj.Key("Subtype").Name() == "Image" {
			imstrm = obj
		} else {
			panic("not yet implemented")
		}
	case Stream:
		imstrm = im
	default:
		panic("not yet implemented")
	}

	data = imstrm.Reader()
	f := imstrm.Key("Filter")
	switch f.Kind() {
	case Name:
		filters = []FilterType{FilterTypeFromName(f.Name())}
	case Array:
		for i := range f.Len() {
			if f.Index(i).Kind() == Name {
				filters = append(filters, FilterTypeFromName(f.Index(i).Name()))
			} else {
				panic("invalid filters")
			}
		}
	case Null:
	default:
		panic("invalid filters")
	}

	P0 := interp.g.CTM.apply(Point{0, 0})
	P1 := interp.g.CTM.apply(Point{1, 0})
	P2 := interp.g.CTM.apply(Point{1, 1})
	P3 := interp.g.CTM.apply(Point{0, 1})
	return Image{P0, P1, P2, P3, filters, data}
}
