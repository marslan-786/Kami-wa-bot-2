package main

import (
	"strings"
)

var flagMap = map[string]string{
	"afghanistan": "🇦🇫", "albania": "🇦🇱", "algeria": "🇩🇿", "pakistan": "🇵🇰", "venezuela": "🇻🇪",
	"india": "🇮🇳", "usa": "🇺🇸", "uk": "🇬🇧", "russia": "🇷🇺", "canada": "🇨🇦",
    // یہاں آپ مزید کنٹریز ایڈ کر سکتے ہیں جو آپ کی لسٹ میں تھے
}

func GetCountryWithFlag(countryName string) (string, string) {
	cleanName := strings.ToLower(strings.Fields(countryName)[0])
	flag, ok := flagMap[cleanName]
	if !ok {
		return "🌐", "🌐 " + countryName
	}
	return flag, flag + " " + countryName
}