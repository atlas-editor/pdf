package pdf

type matrix [3][3]float64

var ident = matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}

func (x matrix) mul(y matrix) matrix {
	var z matrix
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				z[i][j] += x[i][k] * y[k][j]
			}
		}
	}
	return z
}

func (x matrix) apply(v Point) Point {
	return Point{x[0][0]*v.X + x[1][0]*v.Y + x[2][0], x[0][1]*v.X + x[1][1]*v.Y + x[2][1]}
}
