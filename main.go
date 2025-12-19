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

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var client *whatsmeow.Client
var mongoColl *mongo.Collection
var isFirstRun = true

// --- MongoDB Setup ---
func initMongoDB() {
	uri := "mongodb://mongo:AEvrikOWlrmJCQrDTQgfGtqLlwhwLuAA@crossover.proxy.rlwy.net:29609"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mClient, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		fmt.Println("❌ [MongoDB] Connection Failed!")
		panic(err)
	}
	mongoColl = mClient.Database("kami_otp_db").Collection("sent_otps")
	fmt.Println("✅ [DB] MongoDB Connected for History")
}

func isAlreadySent(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var result bson.M
	err := mongoColl.FindOne(ctx, bson.M{"msg_id": id}).Decode(&result)
	return err == nil
}

func markAsSent(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = mongoColl.InsertOne(ctx, bson.M{"msg_id": id, "at": time.Now()})
}

// --- Monitoring Logic ---
func checkOTPs(cli *whatsmeow.Client) {
	for i, url := range Config.OTPApiURLs {
		apiIdx := i + 1
		httpClient := &http.Client{Timeout: 8 * time.Second}
		resp, err := httpClient.Get(url)
		if err != nil {
			fmt.Printf("⚠️ [API SKIP] API %d unreachable\n", apiIdx)
			continue
		}

		var data map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()
		if data == nil || data["aaData"] == nil {
			continue
		}

		aaData, ok := data["aaData"].([]interface{})
		if !ok || len(aaData) == 0 {
			continue
		}

		apiName := "API-Server"
		if strings.Contains(url, "kamibroken") {
			apiName = "Kami-Broken"
		}

		if isFirstRun {
			fmt.Printf("🚀 [First Run] Syncing %d old records from API %d\n", len(aaData), apiIdx)
			for _, row := range aaData {
				r := row.([]interface{})
				msgID := fmt.Sprintf("%v_%v", r[2], r[0])
				if !isAlreadySent(msgID) {
					markAsSent(msgID)
				}
			}
			isFirstRun = false
			return
		}

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 {
				continue
			}

			msgID := fmt.Sprintf("%v_%v", r[2], r[0])
			if !isAlreadySent(msgID) {
				rawTime, _ := r[0].(string)
				countryRaw, _ := r[1].(string)
				phone, _ := r[2].(string)
				service, _ := r[3].(string)
				fullMsg, _ := r[4].(string)

				cleanCountry := strings.Fields(strings.Split(countryRaw, "-")[0])[0]
				cFlag, _ := GetCountryWithFlag(cleanCountry)
				otpCode := regexp.MustCompile(`\b\d{3,4}[-\s]?\d{3,4}\b|\b\d{4,8}\b`).FindString(fullMsg)
				flatMsg := strings.ReplaceAll(strings.ReplaceAll(fullMsg, "\n", " "), "\r", "")

				// بیک ٹِکس (backticks) والے مسئلے کا حل: ان کو جوڑ کر (Concatenate) لکھا گیا ہے
				bt := "`"
				messageBody := fmt.Sprintf("✨ *%s | %s Message %d*⚡\n"+
					"> ⏰ %sTime%s ~ _%s_\n"+
					"> 🌍 %sCountry%s • _%s_\n"+
					"  📞 %sNumber%s √ _%s_\n"+
					"> ⚙️ %sService%s + _%s_\n"+
					"  🔑 %sOTP%s ✓ *%s*\n"+
					"> 📡 %sAPI%s × *%s*\n"+
					"> 📞 %sjoin for numbers%s\n"+
					"> https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht\n"+
					"> https://chat.whatsapp.com/L0Qk2ifxRFU3fduGA45osD\n"+
					"📩 %sFull Msg%s\n"+
					"> %s%s%s",
					cFlag, strings.ToUpper(service), apiIdx,
					bt, bt, rawTime,
					bt, bt, cFlag+" "+cleanCountry,
					bt, bt, maskNumber(phone),
					bt, bt, service,
					bt, bt, otpCode,
					bt, bt, apiName,
					bt, bt,
					bt, bt,
					bt, flatMsg, bt)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, _ := types.ParseJID(jidStr)
					cli.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
					time.Sleep(2 * time.Second)
				}
				markAsSent(msgID)
				fmt.Printf("✅ [Sent] API %d OTP for %s\n", apiIdx, phone)
			}
		}
	}
}

func main() {
	fmt.Println("🚀 [Init] Starting...")
	initMongoDB()

	dbURL := os.Getenv("DATABASE_URL")
	dbType := "postgres"

	if dbURL == "" {
		fmt.Println("ℹ️ No DATABASE_URL found, using local SQLite")
		dbURL = "file:kami_session.db?_foreign_keys=on"
		dbType = "sqlite3"
	}

	dbLog := waLog.Stdout("Database", "INFO", true)
	// فکسڈ: شروع میں context.Background() ایڈ کر دیا گیا ہے
	container, err := sqlstore.New(context.Background(), dbType, dbURL, dbLog)
	if err != nil {
		fmt.Printf("❌ [DB Error] %v\n", err)
		return
	}

	// فکسڈ: GetFirstDevice میں context.Background() ایڈ کر دیا گیا ہے
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(err)
	}

	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "INFO", true))
	client.AddEventHandler(func(evt interface{}) {})

	err = client.Connect()
	if err != nil {
		panic(err)
	}

	if client.Store.ID == nil {
		code, _ := client.PairPhone(context.Background(), Config.OwnerNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		fmt.Printf("\n🔑 PAIRING CODE: %s\n\n", code)
	}

	go func() {
		for {
			if client.IsLoggedIn() {
				checkOTPs(client)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	client.Disconnect()
}