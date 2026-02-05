package main

var Config = struct {
	OwnerNumber   string
	BotName       string
	OTPChannelIDs []string
	OTPApiURLs    []string
	Interval      int
}{
	OwnerNumber: "923027665767",
	BotName:     "Kami OTP Monitor",
	OTPChannelIDs: []string{
		"120363423661360002@newsletter",
	},
	OTPApiURLs: []string{
		"https://api-node-js-new-production-b09a.up.railway.app/api/sms",
	},
	Interval: 4,
}
