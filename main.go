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

	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

var client *whatsmeow.Client
var lastProcessedIDs = make(map[string]bool)

// --- Helper Functions ---
func extractOTP(msg string) string {
	re := regexp.MustCompile(`\b\d{3,4}[-\s]?\d{3,4}\b|\b\d{4,8}\b`)
	return re.FindString(msg)
}

func maskNumber(num string) string {
	if len(num) < 7 { return num }
	return num[:5] + "XXXX" + num[len(num)-2:]
}

func checkOTPs(cli *whatsmeow.Client) {
	for _, url := range Config.OTPApiURLs {
		resp, err := http.Get(url)
		if err != nil { continue }
		defer resp.Body.Close()

		var data map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&data)

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
					cli.SendMessage(context.Background(), jid, &waProto.Message{
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
		} else if msgText == ".chk" {
			client.ReplyMessage(v, "🧪 *Go Bot Test*\n\n1. Copy OTP: `123456`\n2. Group: https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht")
		}
	}
}

func main() {
	dbURL := os.Getenv("DATABASE_URL") // ریلوے سے خود بخود ملے گا
	if dbURL == "" {
		fmt.Println("❌ DATABASE_URL missing!")
		return
	}

	dbLog := sqlstore.NewLogger(nil, "INFO")
	container, err := sqlstore.New("postgres", dbURL, dbLog)
	if err != nil { panic(err) }
	
	deviceStore, err := container.GetFirstDevice()
	if err != nil { panic(err) }

	client = whatsmeow.NewClient(deviceStore, nil)
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("⏳ Pairing for:", Config.OwnerNumber)
		time.Sleep(3 * time.Second)
		code, _ := client.PairCode(Config.OwnerNumber, true, whatsmeow.PairCodeMethodChrome, "Chrome (Linux)")
		fmt.Printf("\n🔑 YOUR PAIRING CODE: \033[1;32m%s\033[0m\n\n", code)
	} else {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("✅ Bot Connected!")
		go func() {
			for {
				checkOTPs(client)
				time.Sleep(time.Duration(Config.Interval) * time.Second)
			}
		}()
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}