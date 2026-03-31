package app

func renderErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
