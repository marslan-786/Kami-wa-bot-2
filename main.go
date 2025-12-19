package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

var client *whatsmeow.Client
var lastProcessedIDs = make(map[string]bool)

func extractOTP(msg string) string {
	re := regexp.MustCompile(`\b\d{3,4}[-\s]?\d{3,4}\b|\b\d{4,8}\b`)
	match := re.FindString(msg)
	if match == "" { return "N/A" }
	return match
}

func maskNumber(num string) string {
	if len(num) < 7 { return num }
	return num[:5] + "XXXX" + num[len(num)-2:]
}

func checkOTPs() {
	for _, url := range Config.OTPApiURLs {
		resp, err := http.Get(url)
		if err != nil { continue }
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var data map[string]interface{}
		json.Unmarshal(body, &data)

		aaData, ok := data["aaData"].([]interface{})
		if !ok { continue }

		apiName := "API 1"
		if strings.Contains(url, "kamibroken") { apiName = "API 2" }

		for _, row := range aaData {
			r := row.([]interface{})
			if len(r) < 5 { continue }

			msgID := fmt.Sprintf("%v_%v", r[2], r[0])
			if !lastProcessedIDs[msgID] {
				rawTime, countryInfo, phone, service, fullMsg := r[0].(string), r[1].(string), r[2].(string), r[3].(string), r[4].(string)
				cFlag, countryWithFlag := GetCountryWithFlag(countryInfo)
				otpCode := extractOTP(fullMsg)

				messageBody := fmt.Sprintf(`
✨ *%s | %s New Message Received %s*⚡

> ⏰   *`+"`Time`"+`   •   _%s_*

> 🌍   *`+"`Country`"+`  ✓   _%s_*

  📞   *`+"`Number`"+`  √   _%s_*

> ⚙️   *`+"`Service`"+`  ©   _%s_*

  🔑   *`+"`OTP`"+`  ~   _%s_*
  
> 📋   *`+"`Join For Numbers`"+`*
  
> https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht

> 📩   `+"`Full Message`"+`

> `+"`%s`"+`

> Developed by Nothing Is Impossible

> `+"`🙂MR~Bunny🙂`"+` `+"`💔Um@R💔`"+` `+"`👑Mohsin~King👑`"+` 
> `+"`😎SK~SuFyAn😎`"+` `+"`😈SUDAIS~Ahmed👿`"+`
`, cFlag, strings.ToUpper(service), apiName, rawTime, countryWithFlag, maskNumber(phone), service, otpCode, fullMsg)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, _ := types.ParseJID(jidStr)
					client.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
				}
				lastProcessedIDs[msgID] = true
			}
		}
	}
}

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		msgText := v.Message.GetConversation()
		if msgText == "" { msgText = v.Message.GetExtendedTextMessage().GetText() }

		if msgText == ".id" {
			client.ReplyMessage(v, fmt.Sprintf("📍 *Chat ID:* `%s`", v.Info.Chat))
		} else if msgText == ".chk" || msgText == ".check" {
			client.ReplyMessage(v, "🧪 *Go Bot Test* ⚡\n\n1. Copy OTP: `123456` (Click to copy)\n2. Group: https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht")
		}
	}
}

func main() {
	dbLog := sqlstore.NewLogger(nil, "INFO")
    // والیم کے لیے راستہ سیٹ کیا گیا ہے
	container, err := sqlstore.New("sqlite3", "file:/app/data/kami_store.db?_foreign_keys=on", dbLog)
	if err != nil { panic(err) }
	
	deviceStore, err := container.GetFirstDevice()
	if err != nil { panic(err) }

	client = whatsmeow.NewClient(deviceStore, nil)
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		err = client.Connect()
		if err != nil { panic(err) }
		
		fmt.Println("⏳ Requesting Pairing Code for:", Config.OwnerNumber)
		time.Sleep(3 * time.Second)
		code, _ := client.PairCode(Config.OwnerNumber, true, whatsmeow.PairCodeMethodChrome, "Chrome (Linux)")
		fmt.Printf("\n🔑 YOUR PAIRING CODE: \033[1;32m%s\033[0m\n\n", code)
	} else {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("✅ Bot Connected!")
		go func() {
			for {
				checkOTPs()
				time.Sleep(time.Duration(Config.Interval) * time.Second)
			}
		}()
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	client.Disconnect()
}