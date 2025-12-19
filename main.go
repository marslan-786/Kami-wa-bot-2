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

// --- اے پی آئی چیک کرنے کا مضبوط فنکشن ---
func checkOTPs(cli *whatsmeow.Client) {
	fmt.Println("🔍 [Monitor] Checking APIs...")
	
	for _, url := range Config.OTPApiURLs {
		fmt.Printf("🌐 [Requesting] %s\n", url)
		
		httpClient := http.Client{Timeout: 8 * time.Second}
		resp, err := httpClient.Get(url)
		if err != nil {
			fmt.Printf("⚠️ [API SKIP] Connection error for %s: %v\n", url, err)
			continue 
		}

		var data map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()

		if err != nil {
			fmt.Printf("⚠️ [API SKIP] JSON error for %s: %v\n", url, err)
			continue
		}

		aaData, ok := data["aaData"].([]interface{})
		if !ok {
			fmt.Printf("⚠️ [API SKIP] No data found in %s\n", url)
			continue
		}

		apiName := "API-Server"
		if strings.Contains(url, "kamibroken") { apiName = "Kami-Broken" }

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 { continue }

			msgID := fmt.Sprintf("%v_%v", r[2], r[0])
			if !lastProcessedIDs[msgID] {
				fmt.Printf("📩 [New OTP] Detected from %s for %v\n", apiName, r[2])
				
				rawTime, _ := r[0].(string)
				countryInfo, _ := r[1].(string)
				phone, _ := r[2].(string)
				service, _ := r[3].(string)
				fullMsg, _ := r[4].(string)

				cFlag, countryWithFlag := GetCountryWithFlag(countryInfo)
				otpCode := extractOTP(fullMsg)

				messageBody := fmt.Sprintf(`✨ *%s | %s Message*⚡
> ⏰ Time: _%s_
> 🌍 Country: _%s_
> 📞 Number: _%s_
> ⚙️ Service: _%s_
> 🔑 OTP: *%s*
> 📡 API: *%s*

📩 Full Msg:
"%s"

_Developed by Nothing Is Impossible_`, cFlag, strings.ToUpper(service), rawTime, countryWithFlag, maskNumber(phone), service, otpCode, apiName, fullMsg)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, err := types.ParseJID(jidStr)
					if err != nil { continue }
					
					fmt.Printf("📤 [Sending] To Channel: %s\n", jidStr)
					_, err = cli.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
					if err != nil {
						fmt.Printf("❌ [Send Error] Channel %s: %v\n", jidStr, err)
					}
				}
				lastProcessedIDs[msgID] = true
			}
		}
	}
}

// --- بٹن ٹیسٹنگ (انتہائی مستحکم طریقہ) ---
func sendTestButtons(cli *whatsmeow.Client, chat types.JID) {
	fmt.Printf("🛠 [Test] Sending interactive styles to %s...\n", chat)

	// لیٹسٹ لائبریری کے مطابق "Native Flow" کا سب سے محفوظ ڈھانچہ
	// ہم انٹرایکٹو میسج کو ایک خاص طریقے سے ریپ (Wrap) کر رہے ہیں تاکہ ایرر نہ آئے
	interactiveMsg := &waProto.InteractiveMessage{
		Header: &waProto.InteractiveMessage_Header{
			Title: proto.String("Kami Bot Hub"),
		},
		Body: &waProto.InteractiveMessage_Body{
			Text: proto.String("⚡ *System Status: Online*\n\nChoose an action:"),
		},
		InteractiveMessageConfig: &waProto.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waProto.InteractiveMessage_NativeFlowMessage{
				Buttons: []*waProto.InteractiveMessage_NativeFlowMessage_Button{
					{
						Name: proto.String("cta_copy"),
						ButtonParamsJson: proto.String(`{"display_text":"Copy Test Code","id":"123","copy_code":"TEST-999"}`),
					},
					{
						Name: proto.String("cta_url"),
						ButtonParamsJson: proto.String(`{"display_text":"Official Group","url":"https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht"}`),
					},
				},
			},
		},
	}

	// بلڈ فیل ہونے سے بچنے کے لیے ہم میسج کو صرف تب بھیجیں گے جب ڈھانچہ درست ہو
	msg := &waProto.Message{
		InteractiveMessage: interactiveMsg,
	}

	resp, err := cli.SendMessage(context.Background(), chat, msg)
	if err != nil {
		fmt.Printf("❌ [Button Test Failed]: %v\n", err)
		// Fallback: سادہ ٹیکسٹ میسج
		cli.SendMessage(context.Background(), chat, &waProto.Message{
			Conversation: proto.String("⚠️ Interactive buttons not supported on this device/account. Try simple text commands."),
		})
	} else {
		fmt.Printf("✅ [Button Test Success]: Message ID %s\n", resp.ID)
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
	fmt.Println("🚀 [Boot] Initializing Kami Bot...")
	
	dbLog := waLog.Stdout("Database", "INFO", true)
	// SQLite فائل بنانا
	container, err := sqlstore.New("sqlite3", "file:kami_bot.db?_foreign_keys=on", dbLog)
	if err != nil { panic(err) }
	
	deviceStore, err := container.GetFirstDevice()
	if err != nil { panic(err) }

	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "INFO", true))
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("⏳ [Auth] Waiting for pairing code...")
		// پیرنگ کے لیے لیٹسٹ PairPhone فنکشن
		code, err := client.PairPhone(context.Background(), Config.OwnerNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil { fmt.Printf("❌ [Auth Error]: %v\n", err); return }
		fmt.Printf("\n🔑 YOUR CODE: %s\n\n", code)
	} else {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("✅ [Ready] Bot is online and listening!")
		
		// مانیٹرنگ لوپ
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