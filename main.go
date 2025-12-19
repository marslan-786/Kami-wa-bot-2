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

func extractOTP(msg string) string {
	re := regexp.MustCompile(`\b\d{3,4}[-\s]?\d{3,4}\b|\b\d{4,8}\b`)
	return re.FindString(msg)
}

func maskNumber(num string) string {
	if len(num) < 7 { return num }
	return num[:5] + "XXXX" + num[len(num)-2:]
}

// --- اے پی آئی مانیٹرنگ (Robust Version) ---
func checkOTPs(cli *whatsmeow.Client) {
	fmt.Println("🔍 [Monitor] Cycle started...")
	for _, url := range Config.OTPApiURLs {
		// اے پی آئی ہٹ کرنے کی کوشش
		clientHTTP := http.Client{Timeout: 10 * time.Second}
		resp, err := clientHTTP.Get(url)
		
		if err != nil {
			fmt.Printf("⚠️ [API SKIP] Connection failed for %s: %v\n", url, err)
			continue // اگلی اے پی آئی پر جائیں
		}

		var data map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()

		if err != nil {
			fmt.Printf("⚠️ [API SKIP] JSON Error for %s: %v\n", url, err)
			continue 
		}

		aaData, ok := data["aaData"].([]interface{})
		if !ok {
			fmt.Printf("⚠️ [API SKIP] No aaData in %s\n", url)
			continue
		}

		apiName := "API 1"
		if strings.Contains(url, "kamibroken") { apiName = "API 2" }

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 { continue }

			msgID := fmt.Sprintf("%v_%v", r[2], r[0])
			if !lastProcessedIDs[msgID] {
				fmt.Printf("📩 [New] Found OTP for %v\n", r[2])
				
				rawTime, _ := r[0].(string)
				countryInfo, _ := r[1].(string)
				phone, _ := r[2].(string)
				service, _ := r[3].(string)
				fullMsg, _ := r[4].(string)

				cFlag, countryWithFlag := GetCountryWithFlag(countryInfo)
				otpCode := extractOTP(fullMsg)

				messageBody := fmt.Sprintf(`✨ *%s | %s New Message*⚡
> ⏰ Time: _%s_
> 🌍 Country: _%s_
> 📞 Number: _%s_
> ⚙️ Service: _%s_
> 🔑 OTP: *%s*

📩 Full Msg:
"%s"`, cFlag, strings.ToUpper(service), rawTime, countryWithFlag, maskNumber(phone), service, otpCode, fullMsg)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, _ := types.ParseJID(jidStr)
					_, err := cli.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
					if err != nil {
						fmt.Printf("❌ [Error] Send to %s failed: %v\n", jidStr, err)
					}
				}
				lastProcessedIDs[msgID] = true
			}
		}
	}
}

// --- بٹن ٹیسٹنگ (Updated for Latest Protobuf) ---
func sendTestButtons(cli *whatsmeow.Client, chat types.JID) {
	fmt.Printf("🛠 [Test] Building buttons for %s...\n", chat)

	// لیٹسٹ ورژن میں بٹن اب 'InteractiveMessage' کے اندر 'NativeFlowMessage' میں ہوتے ہیں
	interactiveMsg := &waProto.InteractiveMessage{
		Header: &waProto.InteractiveMessage_Header{
			Title: proto.String("Kami Bot System"),
		},
		Body: &waProto.InteractiveMessage_Body{
			Text: proto.String("⚡ *OTP Test* \n\nSelect a style below:"),
		},
		InteractiveMessageConfig: &waProto.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waProto.InteractiveMessage_NativeFlowMessage{
				Buttons: []*waProto.InteractiveMessage_NativeFlowMessage_Button{
					{
						Name: proto.String("cta_copy"),
						ButtonParamsJson: proto.String(`{"display_text":"Copy OTP","id":"123","copy_code":"456-789"}`),
					},
					{
						Name: proto.String("cta_url"),
						ButtonParamsJson: proto.String(`{"display_text":"Join Group","url":"https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht"}`),
					},
				},
			},
		},
	}

	// میسج کو 'Message' سٹرکچر میں ریپ کریں
	msg := &waProto.Message{
		InteractiveMessage: interactiveMsg,
	}

	resp, err := cli.SendMessage(context.Background(), chat, msg)
	if err != nil {
		fmt.Printf("❌ [Button Error]: %v\n", err)
		// اگر بٹن فیل ہو تو سادہ ٹیکسٹ میسج بھیجیں تاکہ کریش نہ ہو
		cli.SendMessage(context.Background(), chat, &waProto.Message{
			Conversation: proto.String("⚠️ Your account doesn't support interactive buttons. Use simple commands."),
		})
	} else {
		fmt.Printf("✅ [Button Success]: ID %s\n", resp.ID)
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
		} else if msgText == ".chk" || msgText == ".check" {
			sendTestButtons(client, v.Info.Chat)
		}
	}
}

func main() {
	dbLog := waLog.Stdout("Database", "INFO", true)
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:kami_bot.db?_foreign_keys=on", dbLog)
	if err != nil { panic(err) }
	
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil { panic(err) }

	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "INFO", true))
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("⏳ [Auth] Requesting Pairing Code...")
		code, err := client.PairPhone(context.Background(), Config.OwnerNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil { fmt.Printf("❌ [Error] %v\n", err); return }
		fmt.Printf("\n🔑 YOUR PAIRING CODE: %s\n\n", code)
	} else {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("✅ [Ready] Online!")
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
	client.Disconnect()
}