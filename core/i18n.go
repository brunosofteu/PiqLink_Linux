package core

import "sync/atomic"

var curLang atomic.Value // string

// Langs lists the interface languages: code and native name.
var Langs = [][2]string{{"it", "Italiano"}, {"en", "English"}, {"es", "Español"}, {"zh", "中文"}}

// SetLang selects the interface language ("it", "en", "es", "zh"); anything
// else means Italian.
func SetLang(l string) {
	switch l {
	case "en", "es", "zh":
	default:
		l = "it"
	}
	curLang.Store(l)
}

// Lang returns the current language code.
func Lang() string {
	if l, ok := curLang.Load().(string); ok {
		return l
	}
	return "it"
}

// Tr returns the text in the current language. Spanish and Chinese are looked
// up by the English text; English is the fallback.
func Tr(it, en string) string {
	switch Lang() {
	case "it":
		return it
	case "es":
		if t, ok := trTable[en]; ok {
			return t[0]
		}
	case "zh":
		if t, ok := trTable[en]; ok {
			return t[1]
		}
	}
	return en
}
