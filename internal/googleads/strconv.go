package googleads

import "strconv"

func strconvFormatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}

func strconvFormatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}
