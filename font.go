package pdf

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
