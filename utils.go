package pdf

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~float32 | ~float64
}

func findEnclosingRectangle(pts []Point) Rectangle {
	if len(pts) == 0 {
		panic("no points supplied")
	}

	x1, y1, x2, y2 := pts[0].X, pts[0].Y, pts[0].X, pts[0].Y
	for _, pt := range pts[1:] {
		x1 = min(x1, pt.X)
		x2 = max(x2, pt.X)
		y1 = min(y1, pt.Y)
		y2 = max(y2, pt.Y)
	}

	return Rectangle{x1, y1, x2 - x1, y2 - y1}
}

func abs[T Number](x T) T {
	if x < 0 {
		return -x
	}
	return x
}
