package pdf

import (
	"io"
	"iter"
)

// A Char represents a single piece of text drawn on a page.
type Char struct {
	Font     string  // the font used
	FontSize float64 // the font size, in points (1/72 of an inch)
	X        float64 // the X coordinate, in points, increasing left to right
	Y        float64 // the Y coordinate, in points, increasing bottom to top
	W        float64 // the width of the char, in points
	S        string  // the actual UTF-8 text
}

// A Point represents an X, Y pair.
type Point struct {
	X float64
	Y float64
}

// A Rectangle represents a rectangle.
type Rectangle struct {
	X, Y, W, H float64
}

// A Curve represents a general non-rectangle non-line curve .
type Curve struct {
	Segments []Segment
}

// A Line represents a line.
type Line struct {
	X, Y Point
}

type FilterType int

const (
	ASCIIHexDecode FilterType = iota
	ASCII85Decode
	LZWDecode
	FlateDecode
	RunLengthDecode
	CCITTFaxDecode
	JBIG2Decode
	DCTDecode
	JPXDecode
	Crypt
)

var _FilterType_nameMap = map[string]FilterType{
	"ASCIIHexDecode":  ASCIIHexDecode,
	"ASCII85Decode":   ASCII85Decode,
	"LZWDecode":       LZWDecode,
	"FlateDecode":     FlateDecode,
	"RunLengthDecode": RunLengthDecode,
	"CCITTFaxDecode":  CCITTFaxDecode,
	"JBIG2Decode":     JBIG2Decode,
	"DCTDecode":       DCTDecode,
	"JPXDecode":       JPXDecode,
	"Crypt":           Crypt,
}

func FilterTypeFromName(name string) FilterType {
	if val, ok := _FilterType_nameMap[name]; ok {
		return val
	} else {
		panic("unknown filter type")
	}
}

// An Image represents an embedded image with its anchor points wrt to the document's media box.
type Image struct {
	P0, P1, P2, P3 Point
	filters        []FilterType
	Data           io.ReadCloser
}

type SegmentType int

const (
	M SegmentType = iota
	L
	C
	V
	Y
	H
)

// A Segment represents a path's segment, see Table 4.9 (Chap. 4, PDF 1.7 Reference, 6th Ed.).
type Segment struct {
	Type       SegmentType
	Parameters []float64
}

func splitPath(segments []Segment) iter.Seq[[]Segment] {
	idx := -1
	for i, s := range segments {
		if s.Type == M {
			idx = i
			break
		}
	}

	if idx == -1 {
		println("`m` op missing in path")
		return func(yield func([]Segment) bool) {
			return
		}
	}

	return func(yield func([]Segment) bool) {
		subpath := []Segment{segments[idx]}
		for _, s := range segments[idx+1:] {
			if s.Type == M {
				if !yield(subpath) {
					return
				}
				subpath = []Segment{s}
			} else {
				subpath = append(subpath, s)
			}
		}
		if len(subpath) > 0 {
			if !yield(subpath) {
				return
			}
		}
	}
}

func pathMatch(segments []Segment, patterns [][]SegmentType) bool {
patternLoop:
	for _, ops := range patterns {
		if len(segments) != len(ops) {
			continue
		}
		for i, s := range segments {
			if s.Type != ops[i] {
				continue patternLoop
			}
		}
		return true
	}
	return false
}

func isLine(segments []Segment) (Line, bool) {
	if pathMatch(segments, [][]SegmentType{{M, L, H}, {M, L}}) {
		m, l := segments[0], segments[1]
		x1, y1 := m.Parameters[0], m.Parameters[1]
		x2, y2 := l.Parameters[0], l.Parameters[1]
		return Line{Point{x1, y1}, Point{x2, y2}}, true
	}
	return Line{}, false
}

func isRectangle(segments []Segment) (Rectangle, bool) {
	if pathMatch(segments, [][]SegmentType{{M, L, L, L, H}, {M, L, L, L, L}}) {
		m, l1, l2, l3 := segments[0], segments[1], segments[2], segments[3]
		x0, y0 := m.Parameters[0], m.Parameters[1]
		x1, y1 := l1.Parameters[0], l1.Parameters[1]
		x2, y2 := l2.Parameters[0], l2.Parameters[1]
		x3, y3 := l3.Parameters[0], l3.Parameters[1]

		if (x0 == x1 && y1 == y2 && x2 == x3 && y3 == y0) || (y0 == y1 && x1 == x2 && y2 == y3 && x3 == x0) {
			x := min(x0, x1, x2, x3)
			y := min(y0, y1, y2, y3)
			w := abs(x0 - x2)
			h := abs(y0 - y2)
			return Rectangle{x, y, w, h}, true
		}
	}
	return Rectangle{}, false
}
