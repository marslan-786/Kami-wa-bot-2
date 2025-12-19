package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var client *whatsmeow.Client
var lastProcessedIDs = make(map[string]bool)
var isFirstRun = true // پہلی بار چلنے کا فلیگ

func extractOTP(msg string) string {
	re := regexp.MustCompile(`\b\d{3,4}[-\s]?\d{3,4}\b|\b\d{4,8}\b`)
	return re.FindString(msg)
}

func maskNumber(num string) string {
	if len(num) < 7 { return num }
	return num[:5] + "XXXX" + num[len(num)-2:]
}

// --- اے پی آئی چیک کرنے کا فنکشن ---
func checkOTPs(cli *whatsmeow.Client) {
	if cli == nil || !cli.IsConnected() {
		return
	}

	fmt.Println("🔍 [Monitor] API check cycle started...")
	
	for _, url := range Config.OTPApiURLs {
		httpClient := &http.Client{Timeout: 15 * time.Second}
		resp, err := httpClient.Get(url)
		if err != nil {
			fmt.Printf("⚠️ [SKIP] API error: %v\n", err)
			continue 
		}

		var data map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()

		if err != nil {
			continue
		}

		aaData, ok := data["aaData"].([]interface{})
		if !ok || len(aaData) == 0 {
			continue
		}

		apiName := "Server-1"
		if strings.Contains(url, "kamibroken") { apiName = "Kami-Broken" }

		// اگر پہلی بار چل رہا ہے
		if isFirstRun {
			fmt.Println("🚀 [First Run] Marking old messages and sending only the latest one.")
			// تمام آئی ڈیز کو مارک کریں تاکہ پرانی او ٹی پیز نہ جائیں
			for _, row := range aaData {
				r := row.([]interface{})
				msgID := fmt.Sprintf("%v_%v", r[2], r[0])
				lastProcessedIDs[msgID] = true
			}
			// صرف سب سے پہلی (تازہ ترین) او ٹی پی کو دوبارہ 'فالس' کریں تاکہ وہ سینڈ ہو جائے
			latestRow := aaData[0].([]interface{})
			latestID := fmt.Sprintf("%v_%v", latestRow[2], latestRow[0])
			lastProcessedIDs[latestID] = false 
			
			isFirstRun = false // فلیگ بند کر دیں
		}

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 { continue }

			msgID := fmt.Sprintf("%v_%v", r[2], r[0])
			if !lastProcessedIDs[msgID] {
				fmt.Printf("📩 [New OTP] Forwarding from %s\n", apiName)
				
				rawTime, _ := r[0].(string)
				countryInfo, _ := r[1].(string)
				phone, _ := r[2].(string)
				service, _ := r[3].(string)
				fullMsg, _ := r[4].(string)

				// کنٹری نیم صاف کرنا (صرف پہلا لفظ اٹھانا)
				cleanCountry := strings.Split(countryInfo, "-")[0]
				cFlag, _ := GetCountryWithFlag(cleanCountry)
				
				// فل میسج سے انٹر (Newlines) ختم کرنا
				formattedMsg := strings.ReplaceAll(fullMsg, "\n", " ")
				formattedMsg = strings.ReplaceAll(formattedMsg, "\r", "")

				otpCode := extractOTP(fullMsg)

				// آپ کی مخصوص باڈی
				messageBody := fmt.Sprintf(`✨ *%s | %s Message*⚡

> ⏰   *`+"`Time`"+`   •   _%s_*

> 🌍   *`+"`Country`"+`  ✓   _%s_*

  📞   *`+"`Number`"+`  √   _%s_*

> ⚙️   *`+"`Service`"+`  ©   _%s_*

  🔑   *`+"`OTP`"+`  ~   _%s_*

> 📡   *`+"`API`"+`  •   _%s_*
  
> 📋   *`+"`Join For Numbers`"+`*
  
> https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht

📩 Full Msg:
> %s

> Developed by Nothing Is Impossible`, cFlag, strings.ToUpper(service), rawTime, cFlag + " " + cleanCountry, maskNumber(phone), service, otpCode, apiName, formattedMsg)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, err := types.ParseJID(jidStr)
					if err != nil { continue }
					
					_, err = cli.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
					if err == nil {
						fmt.Printf("✅ [Success] OTP forwarded to %s\n", jidStr)
					}
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
			client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
				Conversation: proto.String(fmt.Sprintf("📍 Chat ID: `%s`", v.Info.Chat)),
			})
		}
	}
}

func main() {
	fmt.Println("🚀 [System] Starting Kami OTP Bot...")
	
	dbLog := waLog.Stdout("Database", "INFO", true)
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:kami_bot.db?_foreign_keys=on", dbLog)
	if err != nil { panic(err) }
	
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil { panic(err) }

	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "INFO", true))
	client.AddEventHandler(eventHandler)

	err = client.Connect()
	if err != nil { panic(err) }

	if client.Store.ID == nil {
		fmt.Println("⏳ [Auth] Waiting for pairing code...")
		time.Sleep(3 * time.Second)
		code, err := client.PairPhone(context.Background(), Config.OwnerNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil { fmt.Printf("❌ [Error] %v\n", err); return }
		fmt.Printf("\n🔑 PAIRING CODE: %s\n\n", code)
	} else {
		fmt.Println("✅ [System] Bot Logged In!")
	}

	go func() {
		fmt.Println("⏰ [Scheduler] Loop active.")
		for {
			if client.IsLoggedIn() {
				checkOTPs(client)
			}
			time.Sleep(time.Duration(Config.Interval) * time.Second)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	client.Disconnect()
}